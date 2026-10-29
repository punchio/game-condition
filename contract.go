package condition

import "errors"

// Attr 是属性编号，由业务定义；0 由框架保留（事件 id），业务属性从 1 开始。
type Attr uint16

// Source 是数据域编号，由业务定义；SourceEvent 固定为事件域。
type Source uint16

// SourceEvent 是事件域。业务的状态系统不能映射到该域。
const SourceEvent Source = 1

// eventAttrID 是事件自身的 id 属性（事件域），对应 DSL 的 event_id。
const eventAttrID Attr = 0

// InstanceKey 是条件实例键，在一个 Manager 内必须唯一。
// Manager 只服务一个 owner，业务可以直接使用条件实例 id，或自行编码分区信息。
type InstanceKey = uint64

// Acc 是聚合方式。
// first 与 value 在事件内和跨事件都不同：同一事件内 value 取最后一条匹配记录，
// first 取第一条；写入计数器后 first 保留首次观测，value 被最新观测覆盖。
type Acc uint8

const (
	// AccValue 当前值：取最新一条匹配记录的值；事件型跨事件时覆盖为最新观测值。
	AccValue Acc = iota + 1
	// AccCount 记次数：状态型为匹配记录条数；事件型为事件发生次数。
	AccCount
	// AccSum 求和。
	AccSum
	// AccAvg 求均值。
	AccAvg
	// AccMax 求最大值。
	AccMax
	// AccMin 求最小值。
	AccMin
	// AccFirst 首次取值：累计型只认最早一次观测。
	AccFirst
)

var accNames = [...]string{
	AccValue: "value",
	AccCount: "count",
	AccSum:   "sum",
	AccAvg:   "avg",
	AccMax:   "max",
	AccMin:   "min",
	AccFirst: "first",
}

func (a Acc) String() string {
	if a.Valid() {
		return accNames[a]
	}
	return "?"
}

// Valid 报告 a 是否为合法聚合方式。
func (a Acc) Valid() bool { return a >= AccValue && a <= AccFirst }

// needsAttr 报告该聚合是否需要读取属性值。
func (a Acc) needsAttr() bool { return a != AccCount }

var aggByName = map[string]Acc{
	"value": AccValue,
	"sum":   AccSum,
	"avg":   AccAvg,
	"max":   AccMax,
	"min":   AccMin,
	"first": AccFirst,
}

// LeafState 是一个叶在本轮求值中的进度。
type LeafState struct {
	Node  uint16 // 叶在表达式内的编号，从 0 开始按书写顺序递增
	Has   bool   // 是否取到值；false 表示无值，该叶必定不满足
	Value int64
}

// Result 是一次求值结果。
// Progress 每次求值独立分配，可直接保留；需要显式独立副本时仍可 Clone。
type Result struct {
	Match    bool
	Progress []LeafState
	Round    uint64
}

// Clone 返回 Progress 的独立副本。
func (r Result) Clone() Result {
	if len(r.Progress) == 0 {
		return r
	}
	out := make([]LeafState, len(r.Progress))
	copy(out, r.Progress)
	r.Progress = out
	return r
}

// Handler 是条件更新回调；返回 true 表示条件完成，框架随即停止监听并清理计数。
type Handler func(Result) bool

// Counter 是计数存储，由业务注入，可落在内存、Redis 或 DB。
//
//   - k 是条件实例键，在一个 Manager 内唯一；
//   - index 是事件叶在表达式内的编号，与 LeafState.Node 一致；状态叶占号但不读写；
//   - 同一 (k, index) 上的 acc 必须前后一致，否则 Read 返回 has=false；
//   - Read 的 has=false 表示从未观测，条件据此判定为不满足；
//   - Del 删除该实例的全部计数，条件结束时由框架调用。
type Counter interface {
	Update(k InstanceKey, index uint16, acc Acc, val int64)
	Read(k InstanceKey, index uint16, acc Acc) (val int64, has bool)
	Del(k InstanceKey)
}

// Query 描述一次常驻状态查询，由业务在 StateSource.Eval 中实现。
//
// Index 是框架从纯 AND 等值谓词推导出的精确候选（属性 → 候选值），只用于
// 排除明确不可能的记录，业务可选用；它不会漏掉任何可能命中的记录。
type Query struct {
	Source Source
	Filter Filter
	Agg    Acc
	Attr   Attr
	Index  map[Attr][]int64
}

// StateSource 是常驻状态读取入口，由业务实现。
// 事件记录由框架自行过滤与聚合，不经过 StateSource。
type StateSource interface {
	Eval(q Query) (val int64, has bool)
}

// Expr 是解析好的条件表达式：不可变，可跨 Manager 复用。
// 它由 Parse 产出，同时持有编译好的运行计划。
type Expr interface {
	cond()
	plan() *program
	signature() string
	config() *ParseConfig
}

var (
	errNilExpr     = errors.New("condition: 条件表达式为空")
	errDuplicate   = errors.New("condition: 该实例已注册")
	errUnknownPlan = errors.New("condition: 表达式未登记到计划表（加载配置时请调用 Plans.Add）")
	errNilCounter  = errors.New("condition: Counter 不能为空")
	errNilPlans    = errors.New("condition: Plans 不能为空")
	errNilManager  = errors.New("condition: Manager 为空")
)
