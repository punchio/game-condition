package condition

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseConfig 是解析所需的元信息，由业务提供。
type ParseConfig struct {
	// Attrs 是属性名到属性编号的映射，如 "item_class" -> Attr(3)。
	Attrs map[string]Attr
	// StateSystems 把状态叶的系统名映射到数据域，如 "Item" -> 物品域。
	// 系统名由业务定义，Event / true / false 是保留字。
	StateSystems map[string]Source
}

func (c *ParseConfig) attr(name string, pos int) (Attr, error) {
	if c == nil || c.Attrs == nil {
		return 0, fmt.Errorf("位置 %d: 未配置属性表，无法解析 %q", pos, name)
	}
	a, ok := c.Attrs[name]
	if !ok {
		return 0, fmt.Errorf("位置 %d: 未知属性 %q", pos, name)
	}
	return a, nil
}

func (c *ParseConfig) validate() error {
	if c == nil {
		return nil
	}
	for name, a := range c.Attrs {
		if a == eventAttrID {
			return fmt.Errorf("condition: 属性 %q 映射到了框架保留属性 %d", name, eventAttrID)
		}
	}
	for name, src := range c.StateSystems {
		switch name {
		case "Event", "true", "false":
			return fmt.Errorf("condition: 状态系统名 %q 是保留字", name)
		}
		if src == SourceEvent {
			return fmt.Errorf("condition: 状态系统 %q 不能映射到事件域 %d", name, SourceEvent)
		}
	}
	return nil
}

func (c *ParseConfig) stateSource(name string) (Source, bool) {
	switch name {
	case "Event", "true", "false":
		return 0, false
	}
	if c == nil {
		return 0, false
	}
	src, ok := c.StateSystems[name]
	return src, ok
}

// Parse 解析表达式配置文本，并在解析期编译成运行计划。
// 返回的 Expr 可被任意 Manager 复用；注册时只做运行期绑定。
//
// 文法与语义见 docs；要点：! > & > |，Event 参数顺序固定，聚合默认 count()，
// 阈值默认 >=1，event_id 必须非 0，own 只能出现在 Event 末尾且只接受状态表达式。
func Parse(text string, cfg *ParseConfig) (Expr, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	toks, err := lex(text)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, cfg: cfg}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tokEOF {
		return nil, p.errf("多余的 %q", p.cur().text)
	}
	return newExpr(root, text, cfg)
}

type tokKind uint8

const (
	tokEOF tokKind = iota
	tokIdent
	tokInt
	tokOp
)

type token struct {
	kind tokKind
	text string
	num  int64
	pos  int
}

func lex(s string) ([]token, error) {
	toks := make([]token, 0, 32)
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c >= '0' && c <= '9':
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			n, err := strconv.ParseInt(s[i:j], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("位置 %d: 非法整数 %q", i, s[i:j])
			}
			toks = append(toks, token{kind: tokInt, text: s[i:j], num: n, pos: i})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			toks = append(toks, token{kind: tokIdent, text: s[i:j], pos: i})
			i = j
		default:
			op, n := lexOp(s[i:])
			if n == 0 {
				return nil, fmt.Errorf("位置 %d: 非法字符 %q", i, c)
			}
			toks = append(toks, token{kind: tokOp, text: op, pos: i})
			i += n
		}
	}
	toks = append(toks, token{kind: tokEOF, pos: len(s)})
	return toks, nil
}

func lexOp(s string) (string, int) {
	for _, op := range [...]string{"!=", ">=", "<="} {
		if strings.HasPrefix(s, op) {
			return op, 2
		}
	}
	switch s[0] {
	case '&', '|', '!', '(', ')', ',', '=', '<', '>', '-':
		return s[:1], 1
	}
	return "", 0
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

type parser struct {
	toks []token
	i    int
	cfg  *ParseConfig
}

func (p *parser) cur() token { return p.toks[p.i] }

func (p *parser) at(off int) token {
	if p.i+off >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[p.i+off]
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("位置 %d: %s", p.cur().pos, fmt.Sprintf(format, args...))
}

func (p *parser) isOp(op string) bool {
	t := p.cur()
	return t.kind == tokOp && t.text == op
}

func (p *parser) acceptOp(op string) bool {
	if p.isOp(op) {
		p.i++
		return true
	}
	return false
}

func (p *parser) expectOp(op string) error {
	if !p.acceptOp(op) {
		return p.errf("期望 %q", op)
	}
	return nil
}

func (p *parser) expectIdent() (string, error) {
	t := p.cur()
	if t.kind != tokIdent {
		return "", p.errf("期望标识符")
	}
	p.i++
	return t.text, nil
}

func (p *parser) expectInt() (int64, error) {
	t := p.cur()
	if t.kind != tokInt {
		return 0, p.errf("期望整数")
	}
	p.i++
	return t.num, nil
}

func (p *parser) expectSignedInt() (int64, error) {
	neg := p.acceptOp("-")
	n, err := p.expectInt()
	if err != nil {
		return 0, err
	}
	if neg {
		return -n, nil
	}
	return n, nil
}

// ---- 表达式 ----

func (p *parser) parseOr() (exprNode, error) {
	head, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	if !p.isOp("|") {
		return head, nil
	}
	kids := []exprNode{head}
	for p.acceptOp("|") {
		e, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		kids = append(kids, e)
	}
	return newOr(kids...), nil
}

func (p *parser) parseAnd() (exprNode, error) {
	head, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if !p.isOp("&") {
		return head, nil
	}
	kids := []exprNode{head}
	for p.acceptOp("&") {
		e, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		kids = append(kids, e)
	}
	return newAnd(kids...), nil
}

func (p *parser) parseUnary() (exprNode, error) {
	if p.acceptOp("!") {
		kid, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return newNot(kid), nil
	}
	if p.acceptOp("(") {
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	t := p.cur()
	if t.kind != tokIdent {
		return nil, p.errf("期望状态叶、Event、true 或 false")
	}
	switch t.text {
	case "Event":
		p.i++
		return p.parseEventLeaf()
	case "true":
		p.i++
		return exprTrue, nil
	case "false":
		p.i++
		return exprFalse, nil
	}
	if src, ok := p.cfg.stateSource(t.text); ok {
		p.i++
		return p.parseStateLeaf(src)
	}
	return nil, p.errf("未知叶子 %q", t.text)
}

// parseStateLeaf 解析状态叶：<stateSystem> "(" filter ["," agg] ")" [阈值]。
func (p *parser) parseStateLeaf(src Source) (exprNode, error) {
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	if p.isOp(")") {
		return nil, p.errf("状态叶必须带过滤条件（不支持空记录集）")
	}
	root, err := p.parseFilter()
	if err != nil {
		return nil, err
	}
	flt := newFilter(src, root)
	agg, attr := AccCount, Attr(0)
	if p.acceptOp(",") {
		if agg, attr, err = p.parseAgg(); err != nil {
			return nil, err
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	cmp, vals, err := p.parseThreshold()
	if err != nil {
		return nil, err
	}
	return newLeaf(newStateReader(src, flt, attr, agg), agg, cmp, vals...), nil
}

// parseEventLeaf 解析事件叶：Event(eventMatch [, agg] [, own]) [阈值]。
func (p *parser) parseEventLeaf() (exprNode, error) {
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	var id int64
	t := p.cur()
	switch {
	case t.kind == tokIdent && t.text == "event_id":
		p.i++
		if err := p.expectOp("="); err != nil {
			return nil, err
		}
		v, err := p.expectInt()
		if err != nil {
			return nil, p.errf("期望事件 id")
		}
		id = v
	case t.kind == tokInt:
		p.i++
		id = t.num
	default:
		return nil, p.errf("Event 需要 event_id")
	}
	if id == 0 {
		return nil, p.errf("event_id 不能为 0")
	}
	var (
		root filterExpr
		err  error
	)
	if p.acceptOp("&") {
		if root, err = p.parseFilter(); err != nil {
			return nil, err
		}
	}
	flt := newFilter(SourceEvent, root)
	agg, attr := AccCount, Attr(0)
	var own exprNode
	if p.acceptOp(",") {
		if p.isOwnCall() {
			if own, err = p.parseOwn(); err != nil {
				return nil, err
			}
		} else {
			if agg, attr, err = p.parseAgg(); err != nil {
				return nil, err
			}
			if p.acceptOp(",") {
				if own, err = p.parseOwn(); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	cmp, vals, err := p.parseThreshold()
	if err != nil {
		return nil, err
	}
	return newLeaf(newEventReader(id, flt, attr, own), agg, cmp, vals...), nil
}

// parseAgg 解析聚合：count() 不带属性，其余必须带一个属性。
func (p *parser) parseAgg() (Acc, Attr, error) {
	t := p.cur()
	if t.kind != tokIdent {
		return 0, 0, p.errf("期望聚合函数")
	}
	p.i++
	if err := p.expectOp("("); err != nil {
		return 0, 0, err
	}
	if t.text == "count" {
		if !p.isOp(")") {
			return 0, 0, p.errf("count() 不接受属性")
		}
		p.i++
		return AccCount, 0, nil
	}
	agg, ok := aggByName[t.text]
	if !ok {
		return 0, 0, p.errf("未知聚合函数 %q", t.text)
	}
	pos := p.cur().pos
	attrName, err := p.expectIdent()
	if err != nil {
		return 0, 0, err
	}
	a, err := p.cfg.attr(attrName, pos)
	if err != nil {
		return 0, 0, err
	}
	if err := p.expectOp(")"); err != nil {
		return 0, 0, err
	}
	return agg, a, nil
}

// parseOwn 解析前置条件：只接受状态表达式，文法上不能包含 Event。
func (p *parser) parseOwn() (exprNode, error) {
	if !p.isOwnCall() {
		return nil, p.errf("期望 own(...)")
	}
	p.i++ // own
	if err := p.expectOp("("); err != nil {
		return nil, err
	}
	if p.isOp(")") {
		return nil, p.errf("own(...) 不能为空")
	}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if hasEventLeaf(e) {
		return nil, p.errf("own(...) 内不能使用 Event")
	}
	if err := p.expectOp(")"); err != nil {
		return nil, err
	}
	return e, nil
}

// ---- 记录过滤 ----

// parsePredicate 解析一个属性谓词；裸属性名表示 != 0。
func (p *parser) parsePredicate() (*predExpr, error) {
	t := p.cur()
	if t.kind != tokIdent {
		return nil, p.errf("期望属性名")
	}
	p.i++
	a, err := p.cfg.attr(t.text, t.pos)
	if err != nil {
		return nil, err
	}
	op, vals, ok, err := p.parseCond()
	if err != nil {
		return nil, err
	}
	if !ok {
		op, vals = "!=", []int64{0}
	}
	cmp, out, err := parseCmp(op, vals...)
	if err != nil {
		return nil, p.errf("%v", err)
	}
	return &predExpr{attr: a, cmp: cmp, values: out}, nil
}

func (p *parser) parseFilter() (filterExpr, error) { return p.parseFilterOr() }

func (p *parser) parseFilterOr() (filterExpr, error) {
	head, err := p.parseFilterAnd()
	if err != nil {
		return nil, err
	}
	if !p.isOp("|") {
		return head, nil
	}
	kids := []filterExpr{head}
	for p.acceptOp("|") {
		kid, err := p.parseFilterAnd()
		if err != nil {
			return nil, err
		}
		kids = append(kids, kid)
	}
	return &orExpr{kids: kids}, nil
}

func (p *parser) parseFilterAnd() (filterExpr, error) {
	head, err := p.parseFilterUnary()
	if err != nil {
		return nil, err
	}
	if !p.isOp("&") {
		return head, nil
	}
	kids := []filterExpr{head}
	for p.acceptOp("&") {
		kid, err := p.parseFilterUnary()
		if err != nil {
			return nil, err
		}
		kids = append(kids, kid)
	}
	return &andExpr{kids: kids}, nil
}

func (p *parser) parseFilterUnary() (filterExpr, error) {
	if p.acceptOp("!") {
		kid, err := p.parseFilterUnary()
		if err != nil {
			return nil, err
		}
		return &notExpr{kid: kid}, nil
	}
	if p.acceptOp("(") {
		n, err := p.parseFilterOr()
		if err != nil {
			return nil, err
		}
		if err := p.expectOp(")"); err != nil {
			return nil, err
		}
		return n, nil
	}
	return p.parsePredicate()
}

// parseCond 解析配置层算子与取值；无比较运算时 ok=false。
func (p *parser) parseCond() (string, []int64, bool, error) {
	op, hasOp := p.cmpToken()
	if !hasOp {
		if p.cur().kind != tokInt && !(p.isOp("-") && p.at(1).kind == tokInt) {
			return "", nil, false, nil
		}
		lo, err := p.expectSignedInt()
		if err != nil {
			return "", nil, false, err
		}
		if !p.isOp("-") {
			return "", nil, false, p.errf("缺少比较运算")
		}
		p.i++
		hi, err := p.expectSignedInt()
		if err != nil {
			return "", nil, false, err
		}
		return "range", []int64{lo, hi}, true, nil
	}
	p.i++
	vals, err := p.parseValueList()
	if err != nil {
		return "", nil, false, err
	}
	if p.isOp("-") {
		return "", nil, false, p.errf("区间不能使用比较运算符前缀")
	}
	return op, vals, true, nil
}

func (p *parser) parseValueList() ([]int64, error) {
	first, err := p.expectSignedInt()
	if err != nil {
		return nil, err
	}
	vals := []int64{first}
	for p.valueFollowsComma() {
		p.i++
		v, err := p.expectSignedInt()
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return vals, nil
}

func (p *parser) valueFollowsComma() bool {
	if !p.isOp(",") {
		return false
	}
	n := p.at(1)
	if n.kind == tokInt {
		return true
	}
	return n.kind == tokOp && n.text == "-" && p.at(2).kind == tokInt
}

func (p *parser) parseThreshold() (cmpOp, []int64, error) {
	op, vals, ok, err := p.parseCond()
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		return parseCmp(">=", 1)
	}
	cmp, out, err := parseCmp(op, vals...)
	if err != nil {
		return 0, nil, p.errf("%v", err)
	}
	return cmp, out, nil
}

func (p *parser) isOwnCall() bool {
	t := p.cur()
	return t.kind == tokIdent && t.text == "own" && p.at(1).kind == tokOp && p.at(1).text == "("
}

func (p *parser) cmpToken() (string, bool) {
	t := p.cur()
	if t.kind != tokOp {
		return "", false
	}
	switch t.text {
	case "=", "!=", "<", "<=", ">", ">=":
		return t.text, true
	}
	return "", false
}
