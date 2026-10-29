package condition

import "errors"

var (
	// ErrEventIDZero 表示事件 id 传了 0；0 保留为“非事件”。
	ErrEventIDZero = errors.New("condition: 事件 id 不能为 0")
	// ErrEventDuplicated 表示一个 ChangeSet 里设置了多个事件。
	ErrEventDuplicated = errors.New("condition: 一个 ChangeSet 只能描述一个事件")
	// ErrEventMissing 表示追加事件记录前没有设置事件 id。
	ErrEventMissing = errors.New("condition: 追加事件记录前请先调用 Event")
	// ErrReservedAttr 表示业务把框架保留的事件属性（0，事件 id）当普通属性上报。
	ErrReservedAttr = errors.New("condition: 事件属性 0 由框架保留，业务属性请从 1 开始")
)

// ChangeKey 是一个依赖点：(域, 属性, 值)。
type ChangeKey struct {
	src      Source
	attr     Attr
	id       int64
	wildcard bool
}

// ExactKey 构造精确依赖点：该域该属性取到指定值。
func ExactKey(src Source, attr Attr, id int64) ChangeKey {
	return ChangeKey{src: src, attr: attr, id: id}
}

// WildcardKey 构造通配依赖点：该域该属性任意取值变化都命中。
func WildcardKey(src Source, attr Attr) ChangeKey {
	return ChangeKey{src: src, attr: attr, wildcard: true}
}

type dataValue struct {
	attr  Attr
	value int64
}

type eventRecord struct {
	values []dataValue
}

func (r eventRecord) get(attr Attr) (int64, bool) {
	for _, v := range r.values {
		if v.attr == attr {
			return v.value, true
		}
	}
	return 0, false
}

// ChangeSet 描述一次业务变化。
//
// 状态变化用 State/Mark 上报；事件用 Event 开始、EventValue 追加记录。
// 一次 Trigger 只描述一个事件；多个事件请分别 Trigger。
type ChangeSet struct {
	// event 是本次事件 id，0 表示非事件。它是 ChangeSet 的语义字段，
	// 不只靠 keys 里的依赖点表达：手动 Mark 事件域的 Key 不会被当成一次事件。
	event   int64
	keys    []ChangeKey
	records []eventRecord
}

// NewChangeSet 创建空的变更集。
func NewChangeSet() *ChangeSet { return &ChangeSet{} }

// Event 设置本次触发的事件 id，0 非法。
func (cs *ChangeSet) Event(id int64) error {
	if id == 0 {
		return ErrEventIDZero
	}
	if cs.event != 0 {
		return ErrEventDuplicated
	}
	cs.event = id
	cs.Mark(ExactKey(SourceEvent, eventAttrID, id))
	return nil
}

// EventValue 复制并追加一条完整事件记录，同时生成精确索引元数据。
// 返回后调用方可以立即复用原 map。
func (cs *ChangeSet) EventValue(values map[Attr]int64) error {
	if cs == nil || cs.event == 0 {
		return ErrEventMissing
	}
	r := eventRecord{values: make([]dataValue, 0, len(values))}
	for attr, value := range values {
		if attr == eventAttrID {
			return ErrReservedAttr
		}
		r.values = append(r.values, dataValue{attr: attr, value: value})
	}
	for _, v := range r.values {
		cs.Mark(ExactKey(SourceEvent, v.attr, v.value))
	}
	cs.records = append(cs.records, r)
	return nil
}

// Mark 加入一个候选依赖点，重复的 Key 自动忽略。
func (cs *ChangeSet) Mark(k ChangeKey) {
	for i := range cs.keys {
		if cs.keys[i] == k {
			return
		}
	}
	cs.keys = append(cs.keys, k)
}

// State 记录一次状态变更：old 与 new 两个精确点都会成为候选。
// 调用前请先完成业务数据更新，状态叶读到的是最新数据。
func (cs *ChangeSet) State(src Source, attr Attr, old, new int64) {
	cs.Mark(ExactKey(src, attr, old))
	if old != new {
		cs.Mark(ExactKey(src, attr, new))
	}
}

// Reset 清空变更集并复用底层缓冲。
func (cs *ChangeSet) Reset() {
	clear(cs.records)
	cs.records = cs.records[:0]
	cs.keys = cs.keys[:0]
	cs.event = 0
}

// Empty 报告变更集是否为空。
func (cs *ChangeSet) Empty() bool { return cs == nil || len(cs.keys) == 0 }

func (cs *ChangeSet) empty() bool { return cs.Empty() }

// eventID 返回本次变化携带的事件 id；0 表示不是事件触发。
func (cs *ChangeSet) eventID() int64 {
	if cs == nil {
		return 0
	}
	return cs.event
}

// clone 深拷贝事件记录，供 Handler 内排队触发使用。
func (cs *ChangeSet) clone() *ChangeSet {
	if cs == nil {
		return nil
	}
	out := &ChangeSet{
		event: cs.event,
		keys:  append([]ChangeKey(nil), cs.keys...),
	}
	if len(cs.records) != 0 {
		out.records = make([]eventRecord, len(cs.records))
		for i, r := range cs.records {
			out.records[i].values = append([]dataValue(nil), r.values...)
		}
	}
	return out
}
