# ISA 国际标准大气服务

Go 1.22 + Gin 实现的常驻 HTTP 小服务，按国际标准大气（ISA）模型返回
指定高度的温度、气压、密度、声速与密度高度。**无持久化**，每次请求
独立计算；模型域为 **0 m（平均海平面）～ 20000 m**，不向外推。

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

### 沿航迹累积（飞行任务复盘）

```
POST /api/v1/atmosphere/trajectory/accumulate
Content-Type: application/json
```

请求体是一条**首尾相接**的航段链。每段给 `start_time_s` 加
`end_time_s`（或等价的 `duration_s`），以及起止几何高度
`start_altitude_m` / `end_altitude_m`；段内高度随时间线性变化（恒定
升降率，起止等高即平飞）。后一段的起点时刻与高度必须接得上前一段的
终点（容差 1e-9 相对），接不上则整单拒收并指明断在哪一段；任何高度
越出 0～20000 m 模型域同样整单拒收，**绝不外推**。

```json
{
  "segments": [
    {"start_time_s": 0,    "end_time_s": 660,  "start_altitude_m": 0,     "end_altitude_m": 11000},
    {"start_time_s": 660,  "duration_s": 3000, "start_altitude_m": 11000, "end_altitude_m": 11000},
    {"start_time_s": 3660, "end_time_s": 4200, "start_altitude_m": 11000, "end_altitude_m": 3000}
  ]
}
```

返回**总账 + 逐段小账**。核心量：

- `column_mass_kg_m2` —— 穿过的空气柱质量（单位面积），即密度沿几何
  高度的积分 ∫ρ|dh|。飞机沿时间飞、按高度穿层，段内两者靠恒定升降率
  `r = dh/dt` 联系：`dt = dh/r`，时间积分由此化为高度积分。下降段同样
  "穿过"空气，故取绝对值、沿程只增不减；**平飞不穿越任何高度，贡献
  精确为 0**；爬上去再飞回来，柱质量是单程的两倍。
- `mean_pressure_pa` / `mean_density_kg_m3` —— 按时间加权的平均气压 /
  平均密度（段内为该段时长加权，总体为全程时长加权），回答"这趟任务
  整体上处在多稠的空气里"。

```json
{
  "segment_count": 3,
  "start_time_s": 0, "end_time_s": 4200, "duration_s": 4200,
  "min_altitude_m": 0, "max_altitude_m": 11000,
  "total": {
    "column_mass_kg_m2": 12865.702934662624,
    "mean_pressure_pa": 30206.659661389156,
    "mean_density_kg_m3": 0.45238203227175733
  },
  "segments": [
    {"index": 0, "start_time_s": 0, "end_time_s": 660, "duration_s": 660,
     "start_altitude_m": 0, "end_altitude_m": 11000,
     "climb_rate_m_s": 16.666666666666668,
     "column_mass_kg_m2": 8024.448655051954,
     "mean_pressure_pa": 54312.132648108054, "mean_density_kg_m3": 0.7294953322774503},
    {"index": 1, "...": "巡航段 column_mass_kg_m2 精确为 0，均值即 11000 m 点值"},
    {"index": 2, "...": "下降段 column_mass_kg_m2 4841.25，与爬升同向累加"}
  ]
}
```

数值方法：自适应 Simpson 积分，相对容差 1e-12；被积函数就是模型自己
的 `standardPressureAt` / `standardDensityAt`（与单点查询同一套公式、
同一批常数，平飞段均值与单点接口**逐位一致**）。**对流层顶 11000 m
是强制积分节点**：模型在该高度连续但气压、密度对高度的变化率有拐折，
凡跨界区间一律先在交界处切开、两侧分别积分再相加，绝不用一条光滑
公式硬跨。单次最多 100000 段；数千段的航迹一次请求约几十毫秒。

### 错误（结构化，HTTP 400）

```json
{"error":{"code":"ALTITUDE_BELOW_SEA_LEVEL","message":"altitude -10.000 m is below mean sea level; the model domain is 0..20000 m"}}
```

错误码：`INVALID_PARAMETER`、`INVALID_ALTITUDE`、`ALTITUDE_BELOW_SEA_LEVEL`、
`ALTITUDE_ABOVE_CEILING`、`INVALID_RANGE`、`INVALID_STEP`、
`INVALID_TEMPERATURE_OFFSET`、`DENSITY_OUT_OF_DOMAIN`、
`INVALID_TRAJECTORY`（航迹结构非法：空链、时长非正、段数超限）、
`TRAJECTORY_GAP`（相邻航段在时间或高度上断开，message 指明断点段号）。

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
curl -X POST -H 'Content-Type: application/json' \
  -d '{"segments":[{"start_time_s":0,"end_time_s":660,"start_altitude_m":0,"end_altitude_m":11000},{"start_time_s":660,"duration_s":3000,"start_altitude_m":11000,"end_altitude_m":11000}]}' \
  http://localhost:8080/api/v1/atmosphere/trajectory/accumulate
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
- 10000 m / 20000 m 对表固定值。

沿航迹累积额外钉死：

- 恒高平飞无论多久，穿过的空气柱质量**精确为 0**；
- 0→20000 m 整段 vs 在 11000 m 切成两段分别积分再相加，柱质量在
  1e-9 内一致（积分对分段可加 ⇔ 对流层顶被正确当作强制节点切开）；
- 时间轴整体平移/缩放而高度走法不变，柱质量不跟着变（纯几何积分量）；
- 柱质量与静力学恒等式一致：∫ρ dh = Δp/g（跨交界同样成立）；
- 平飞段的时间加权均值与单点接口在同高度的点值**逐位一致**（同一套
  公式与常数，积分器不另抄）；
- 爬升-下降对称、往返加倍（柱质量沿程累积而非只看端点）；
- 总账 = 逐段小账之和；总均值按段时长加权（可手算核对）；
- 断链（时间/高度跳变）、越界高度、空链、非正时长一律 400 结构化
  拒绝，错误信息指明段号；5000 段长航迹一次算完且精度不变。

## 代码结构

```
cmd/server/main.go                     进程入口
internal/atmosphere/
  constants.go                         所有 ISA 钉死常数（唯一出处）
  errors.go                            结构化错误
  troposphere.go                       对流层公式
  stratosphere.go                      等温层公式
  inversion.go                         密度高度解析反解
  model.go                             Compute / Profile / 声速 / 标准大气点值分派（门面）
  trajectory.go                        航段类型与航迹合法性校验（邻接、域内、时长）
  integrate.go                         自适应 Simpson 积分，对流层顶强制节点切分
  accumulate.go                        逐段与整体累积（柱质量、时间加权均值）
internal/httpapi/
  routes.go                            路由
  handlers.go                          单点/剖面 handler、参数解析
  trajectory.go                        航迹累积 handler、请求体解析
  responses.go                         响应/错误信封
```

依赖已 `vendor/`，Docker 构建无需联网。
