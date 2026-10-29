package condition

import "strings"

// logicOp 是表达式与状态前置条件的逻辑算子。
type logicOp uint8

const (
	logicAnd logicOp = iota
	logicOr
	logicNot
)

// exprNode 是表达式语法树节点，只能由 Parse 组装。
type exprNode interface{ exprNode() }

// leafExpr 是单个依赖叶：状态叶实时取数，事件叶累计事件增量。
type leafExpr struct {
	src    dataReader
	acc    Acc
	cmp    cmpOp
	values []int64
}

func (*leafExpr) exprNode() {}

type logicExpr struct {
	op       logicOp
	children []exprNode
}

func (*logicExpr) exprNode() {}

type constExpr struct{ val bool }

func (*constExpr) exprNode() {}

var (
	exprTrue  exprNode = &constExpr{val: true}
	exprFalse exprNode = &constExpr{val: false}
)

func newLeaf(src dataReader, acc Acc, cmp cmpOp, values ...int64) exprNode {
	return &leafExpr{src: src, acc: acc, cmp: cmp, values: values}
}

func newAnd(children ...exprNode) exprNode {
	return &logicExpr{op: logicAnd, children: children}
}

func newOr(children ...exprNode) exprNode {
	return &logicExpr{op: logicOr, children: children}
}

func newNot(child exprNode) exprNode {
	return &logicExpr{op: logicNot, children: []exprNode{child}}
}

// cond 是 Parse 的产物：语法树 + 编译好的运行计划。
// 语法树保留下来供 own 求值（own 不占用外部叶编号与计数槽位）。
type cond struct {
	root exprNode
	prog *program
	sig  string
	cfg  *ParseConfig
}

func (*cond) cond() {}

func (c *cond) plan() *program { return c.prog }

func (c *cond) signature() string { return c.sig }

func (c *cond) config() *ParseConfig { return c.cfg }

// newExpr 把语法树编译成运行计划。
func newExpr(root exprNode, text string, cfg *ParseConfig) (Expr, error) {
	prog, err := compile(root)
	if err != nil {
		return nil, err
	}
	return &cond{root: root, prog: prog, sig: stripSpace(text), cfg: cfg}, nil
}

// stripSpace 去掉文本中的全部空白，作为表达式签名。
func stripSpace(s string) string {
	if !strings.ContainsAny(s, " \t\r\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// hasEventLeaf 报告表达式里是否含事件叶（own 内不允许）。
func hasEventLeaf(e exprNode) bool {
	switch n := e.(type) {
	case *leafExpr:
		return n.src.kind == readerEvent
	case *logicExpr:
		for _, kid := range n.children {
			if hasEventLeaf(kid) {
				return true
			}
		}
	}
	return false
}
