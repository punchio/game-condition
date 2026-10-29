package condition

import "fmt"

type planKind uint8

const (
	planLeaf planKind = iota + 1
	planAnd
	planOr
	planNot
	planConst
)

// planNode 是不可变表达式结构，以扁平数组 + 子节点下标保存。
type planNode struct {
	kind     planKind
	leaf     uint16
	children []int32
	val      bool
}

// leafPlan 是不可变的叶定义：编译期确定取值器、聚合、比较参数与依赖 Key。
type leafPlan struct {
	node uint16
	kind readerKind
	slot uint16 // 状态叶在 plan.slots 中的下标
	src  dataReader
	acc  Acc
	cmp  cmpOp
	vals []int64
	keys []ChangeKey
}

// program 是表达式编译产物：只读、可跨 Manager 复用。
type program struct {
	nodes      []planNode
	leaves     []leafPlan
	root       int32
	stateCount uint16
}

// compile 把语法树编译成运行计划，只在 Parse 时执行一次。
func compile(root exprNode) (*program, error) {
	if root == nil {
		return nil, fmt.Errorf("condition: 表达式为空")
	}
	p := &program{}
	idx, err := p.build(root)
	if err != nil {
		return nil, err
	}
	p.root = idx
	return p, nil
}

func (p *program) build(e exprNode) (int32, error) {
	switch n := e.(type) {
	case nil:
		return 0, fmt.Errorf("condition: nil 表达式（恒真/恒假请用 true/false）")
	case *leafExpr:
		return p.buildLeaf(n)
	case *logicExpr:
		return p.buildLogic(n)
	case *constExpr:
		return p.newNode(planNode{kind: planConst, val: n.val}), nil
	default:
		return 0, fmt.Errorf("condition: 未知表达式节点 %T", e)
	}
}

func (p *program) newNode(n planNode) int32 {
	idx := int32(len(p.nodes))
	p.nodes = append(p.nodes, n)
	return idx
}

func (p *program) buildLogic(n *logicExpr) (int32, error) {
	if len(n.children) == 0 {
		return 0, fmt.Errorf("condition: 逻辑运算至少需要 1 个子表达式")
	}
	var kind planKind
	switch n.op {
	case logicAnd:
		kind = planAnd
	case logicOr:
		kind = planOr
	case logicNot:
		if len(n.children) != 1 {
			return 0, fmt.Errorf("condition: 取反必须且只能有 1 个子表达式")
		}
		kind = planNot
	default:
		return 0, fmt.Errorf("condition: 未知逻辑算子 %d", n.op)
	}
	idx := p.newNode(planNode{kind: kind})
	kids := make([]int32, 0, len(n.children))
	for _, child := range n.children {
		kid, err := p.build(child)
		if err != nil {
			return 0, err
		}
		kids = append(kids, kid)
	}
	p.nodes[idx].children = kids
	return idx, nil
}

func (p *program) buildLeaf(n *leafExpr) (int32, error) {
	if n.src.kind != readerState && n.src.kind != readerEvent {
		return 0, fmt.Errorf("condition: 叶子缺少取值器")
	}
	if !n.acc.Valid() {
		return 0, fmt.Errorf("condition: 叶子聚合方式非法 %d", uint8(n.acc))
	}
	if err := validateCmp(n.cmp, n.values); err != nil {
		return 0, err
	}
	keys := n.src.keys()
	if len(keys) == 0 {
		return 0, fmt.Errorf("condition: 叶子的取值器没有声明依赖 Key")
	}
	if len(p.leaves) >= 0xFFFF {
		return 0, fmt.Errorf("condition: 叶子数量超过 65535")
	}
	lp := leafPlan{
		node: uint16(len(p.leaves)),
		kind: n.src.kind,
		src:  n.src,
		acc:  n.acc,
		cmp:  n.cmp,
		vals: append([]int64(nil), n.values...),
		keys: append([]ChangeKey(nil), keys...),
	}
	if lp.kind == readerState {
		lp.slot = p.stateCount
		p.stateCount++
	}
	p.leaves = append(p.leaves, lp)
	return p.newNode(planNode{kind: planLeaf, leaf: lp.node}), nil
}

// hasEvent 报告计划里是否含事件叶。
func (p *program) hasEvent() bool {
	for i := range p.leaves {
		if p.leaves[i].kind == readerEvent {
			return true
		}
	}
	return false
}

// evalContext 是一次求值需要的外部世界。
type evalContext struct {
	state   StateSource
	counter Counter
	key     InstanceKey
	plan    *plan
}

// evalPlan 求值一个计划实例，并返回本次求值独有的进度。
// 状态叶读 plan.slots 的共享结果，同一轮内同一查询只调用一次 StateSource.Eval。
func evalPlan(p *plan, ctx *evalContext) (bool, []LeafState) {
	progress := make([]LeafState, len(p.prog.leaves))
	return evalNode(p, p.prog.root, ctx, progress), progress
}

func evalNode(p *plan, i int32, ctx *evalContext, progress []LeafState) bool {
	n := &p.prog.nodes[i]
	switch n.kind {
	case planAnd:
		// 不短路：每个叶的进度都要算出来
		ok := true
		for _, kid := range n.children {
			if !evalNode(p, kid, ctx, progress) {
				ok = false
			}
		}
		return ok
	case planOr:
		ok := false
		for _, kid := range n.children {
			if evalNode(p, kid, ctx, progress) {
				ok = true
			}
		}
		return ok
	case planNot:
		return !evalNode(p, n.children[0], ctx, progress)
	case planConst:
		return n.val
	case planLeaf:
		lp := &p.prog.leaves[n.leaf]
		value, has := leafValue(p, lp, ctx)
		progress[n.leaf] = LeafState{Node: lp.node, Has: has, Value: value}
		return predicate(lp.cmp, lp.vals, value, has)
	}
	return false
}

func readSourceState(slot *sourceState, state StateSource) (int64, bool) {
	if slot == nil {
		return 0, false
	}
	if slot.dirty {
		if state == nil {
			slot.value, slot.has = 0, false
		} else {
			slot.value, slot.has = state.Eval(slot.q)
		}
		slot.dirty = false
	}
	return slot.value, slot.has
}

func leafValue(p *plan, lp *leafPlan, ctx *evalContext) (int64, bool) {
	if lp.kind == readerState {
		return readSourceState(p.slots[lp.slot], ctx.state)
	}
	if ctx.counter == nil {
		return 0, false
	}
	return ctx.counter.Read(ctx.key, lp.node, lp.acc)
}
