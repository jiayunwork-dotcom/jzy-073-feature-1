# ISA 国际标准大气服务

Go 1.22 + Gin 实现的常驻 HTTP 小服务，按国际标准大气（ISA）模型返回
指定高度的温度、气压、密度、声速与密度高度，并支持**沿一整条分段飞行
航迹做时间累积核算**（穿过的空气柱质量、时间加权平均气压/密度/温度）。
**无持久化**，每次请求独立计算；模型域为 **0 m（平均海平面）～
20000 m**，不向外推。

## 大气模型

| 层 | 高度范围 | 温度 | 气压 |
|---|---|---|---|
| 对流层 troposphere | 0 ～ 11000 m | 线性直减 6.5 K/km：`T = T0 - λh` | `p = p0·(T/T0)^(g/(Rλ))` |
| 等温层 stratosphere | 11000 ～ 20000 m | 恒定 216.65 K | `p = p(11km)·exp(-g(h-11km)/(R·Tt))` |

- 钉死常数（`internal/atmosphere/constants.go`，调用方不可覆盖）：
  `T0=288.15 K`、`p0=101325 Pa`、`ρ0=1.225 kg/m³`、`g=9.80665 m/s²`、
  `γ=1.4`；`R` 由海平面基准态导出（≈287.05287 J/(kg·K)，使 ρ0 严格自洽）。
- 密度一律由理想气体状态方程导出：`ρ = p/(R·T)`。
- 声速只跟温度：`a = sqrt(γ·R·T)`，与气压无关。
- 等温层公式直接锚定到对流层公式在 11 km 的求值结果，分层交界
  **按构造连续**（气压差为 0，不存在两本各维护一份的"层顶常数"）。

### 温度偏差（模拟非标准大气日）

`temperature_offset`（K）只加到工作温度上：

- **气压仍走标准气压廓线**；
- 密度用 `p标准 / (R·(T标准+ΔT))` 重算；声速用加偏后的温度；
- ΔT=0 时所有量严格回到纯标准大气。

### 密度高度反算

给定密度，在**标准**大气下解析反解等效几何高度（不是数值迭代）：

- 对流层、等温层分别是各自密度公式的精确解析逆函数，共用同一套常数；
- 无温度偏差时 `密度高度 ≡ 输入高度`（往返误差约 1e-12 m）；
- 叠偏差后密度偏离标准值，密度高度随之偏离几何高度（暖日偏高、冷日偏低），
  这正是密度高度的含义；
- 等效密度超出 0～20 km 标准域时返回 `density_altitude_m: null`，不外推。

## HTTP 接口

### 单点

```
GET /api/v1/atmosphere/point?altitude=<米>[&temperature_offset=<K>]
```

```json
{
  "state": {
    "altitude_m": 10000,
    "layer": "troposphere",
    "temperature_k": 223.15,
    "standard_temperature_k": 223.15,
    "pressure_pa": 26436.24311821699,
    "density_kg_m3": 0.4127061552856171,
    "speed_of_sound_m_s": 299.4631670899094,
    "density_altitude_m": 10000
  }
}
```

### 剖面（区间逐点，含两个端点，前端无需自插中间值）

```
GET /api/v1/atmosphere/profile?start=<米>&end=<米>&step=<米>[&temperature_offset=<K>]
```

当 `end` 不落步长网格时，末端点会作为最后一个点原样补上。单次最多
100000 点。

### 沿飞行剖面累积（航迹核算）

```
POST /api/v1/atmosphere/trajectory
Content-Type: application/json
```

请求体是一串**首尾相接、按时间排序的航段**；段内高度随时间线性（恒定
爬升率/下降率），起止高度相等即恒高平飞。每段给起止时刻，或起点时刻加
`duration_s`（二者只能给一个）：

```json
{
  "temperature_offset_k": 0,
  "segments": [
    {"start_time_s": 0,    "end_time_s": 600,  "start_altitude_m": 0,     "end_altitude_m": 12000},
    {"start_time_s": 600,  "duration_s": 1200, "start_altitude_m": 12000, "end_altitude_m": 12000},
    {"start_time_s": 1800, "end_time_s": 3000, "start_altitude_m": 12000, "end_altitude_m": 0}
  ]
}
```

返回**总账 + 每段小账**，每段含升降率、是否跨越对流层顶、逐段累积量与
两端点的完整 `state`：

| 字段 | 含义 |
|---|---|
| `traversed_air_mass_kg_m2` | 沿航迹穿过的空气柱质量（单位水平截面积，kg/m²）：∫\|ρ dh\|，逐段累加；纯几何量，与时间轴平移/缩放无关 |
| `mean_pressure_pa` / `mean_density_kg_m3` / `mean_temperature_k` | 按**时间加权**的任务平均气压/密度/温度（=∫q dt / 总时长） |

实现要点：

- 每段在几何高度上线性，`dh` 与 `dt` 由恒定升降率一一对应；
- **11 km 对流层顶是强制积分节点**：气压/密度在该点连续但对高度的导数
  拐折，任何跨越该高度的航段都在交界处切开、两侧分别用 **8 点
  Gauss–Legendre 求积**（15 次代数精度）后相加，绝不拿一条光滑公式跨
  拐折硬积；未跨越的航段只有一个光滑片；
- 所有被积点值都走模型唯一的求值内核（即单点 `Compute` 背后的同一份
  公式与同一批钉死常数），积分器不另抄常数、不另立公式；
- 任何端点或中途高度探出 0～20000 m 一律拒绝，**绝不为算完积分而外推**
  （段内线性，端点在域内则中途必在域内）；
- 相邻段必须在时间和高度上都接得上（1e-9 相对容差），断链错误会明确
  指出是第几段接不上第几段；时长必须严格为正；最多 100000 段，请求体
  上限 16 MiB。

```bash
curl -X POST "http://localhost:8080/api/v1/atmosphere/trajectory" \
  -H 'Content-Type: application/json' -d @mission.json
```

### 错误（结构化，HTTP 400）

```json
{"error":{"code":"ALTITUDE_BELOW_SEA_LEVEL","message":"altitude -10.000 m is below mean sea level; the model domain is 0..20000 m"}}
```

错误码：`INVALID_PARAMETER`、`INVALID_ALTITUDE`、`ALTITUDE_BELOW_SEA_LEVEL`、
`ALTITUDE_ABOVE_CEILING`、`INVALID_RANGE`、`INVALID_STEP`、
`INVALID_TEMPERATURE_OFFSET`、`DENSITY_OUT_OF_DOMAIN`、
`INVALID_TRAJECTORY`（航迹缺字段/时长非正/段间接不上等；越界高度仍用
原来的 `ALTITUDE_BELOW_SEA_LEVEL` / `ALTITUDE_ABOVE_CEILING`）。

## 示范算例

10000 m 巡航高度、纯标准大气（可随手对表）：

| 量 | 值 |
|---|---|
| 温度 | **223.15 K（−50.00 ℃）** |
| 气压 | **≈ 26436.24 Pa** |
| 密度 | ≈ 0.4127 kg/m³ |
| 声速 | ≈ 299.46 m/s |

11000 m 对流层顶：T = 216.65 K，p ≈ 22632.04 Pa，ρ ≈ 0.3639 kg/m³；
20000 m：T = 216.65 K，p ≈ 5474.88 Pa。

## 运行

```bash
docker build -t isa-service .
docker run --rm -p 8080:8080 isa-service
# 或本地
go run ./cmd/server          # 默认 :8080，可用 PORT 覆盖
```

```bash
curl "http://localhost:8080/api/v1/atmosphere/point?altitude=10000"
curl "http://localhost:8080/api/v1/atmosphere/profile?start=0&end=20000&step=1000"
```

## 测试

`go test ./...`（已含 `-race` 验证）钉死的判据：

- 海平面精确回到 T0/p0/ρ0；
- 11 km 处对流层与等温层气压差 ≤ 1e-9 Pa（交界连续），密度同样连续；
- 对流层每升 1 km 温度精确降 6.5 K；
- 等温层升温温度恒定、气压密度严格单调下降；
- 同温不同压（含经温度偏移构造）声速相同；
- 对流层顶温度 = 等温层恒定温度 = 216.65 K；
- 密度高度在无偏差时为输入高度的精确逆运算；偏差时按暖高/冷低偏离；
- 负高度、>20 km、坏区间/步长/参数一律 400 结构化拒绝；
- 10000 m / 20000 m 对表固定值；
- **航迹累积不变量**：恒高平飞段穿过的空气柱质量**精确为 0**（与时长
  无关）；地面到模型顶的整段与在 11 km 处人为切两段再相加，所有累积量
  在 1e-12 容差内一致（交界强制切开 + 积分可加性）；时间轴整体平移或
  缩放而高度走法不变时，空气柱质量逐位不变；端点/沿程点值与单点查询
  严格同源一致；跨拐折不切分的单条光滑求积会被测出 1e-6 量级的系统偏
  差，而切分结果对解析原函数误差 ≤ 1e-11；升降同路径质量一致、往返加倍；
- 航迹非法（空航迹、缺字段、end_time/duration 同给或都不给、时长非正、
  段间时间或高度断链、越界、坏温度偏差）一律 400 结构化拒绝并点名航段；
- 4000 段航迹核算远低于 0.5 s。

## 代码结构

```
cmd/server/main.go                     进程入口
internal/atmosphere/
  constants.go                         所有 ISA 钉死常数（唯一出处）
  errors.go                            结构化错误
  troposphere.go                       对流层公式
  stratosphere.go                      等温层公式
  inversion.go                         密度高度解析反解
  model.go                             Compute / Profile / 声速、evaluate 唯一求值内核
  trajectory.go                        航段/航迹领域模型与解析校验（连续性、域）
  integration.go                       交界强制切分 + 8 点 Gauss–Legendre 求积
  accumulate.go                        逐段与整体累积总账
internal/httpapi/
  routes.go                            路由
  handlers.go                          两个 GET handler、参数解析
  trajectory_handler.go                POST /trajectory handler
  responses.go                         响应/错误信封
```

依赖已 `vendor/`，Docker 构建无需联网。
