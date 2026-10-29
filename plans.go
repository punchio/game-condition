package condition

import "errors"

// ErrPlansConfig 表示同一个计划表里混入了不同 ParseConfig 解析出的表达式。
var ErrPlansConfig = errors.New("condition: 同一份 Plans 只能对应一份 ParseConfig")

// Plans 是配置加载期构建的计划表：表达式签名 → 编译产物。
// 表建好后只读，可被所有 Manager 共享。
type Plans struct {
	progs map[string]*program
	cfg   *ParseConfig
}

// NewPlans 创建空计划表。
func NewPlans() *Plans {
	return &Plans{progs: make(map[string]*program)}
}

// Add 登记一份解析结果；签名相同的配置只保留一份编译产物。
func (p *Plans) Add(e Expr) error {
	if e == nil {
		return errNilExpr
	}
	cfg := e.config()
	if p.cfg == nil {
		p.cfg = cfg
	} else if p.cfg != cfg {
		return ErrPlansConfig
	}
	sig := e.signature()
	if _, ok := p.progs[sig]; ok {
		return nil
	}
	p.progs[sig] = e.plan()
	return nil
}

// Len 返回已登记的计划数量。
func (p *Plans) Len() int {
	if p == nil {
		return 0
	}
	return len(p.progs)
}

func (p *Plans) get(sig string) *program {
	if p == nil {
		return nil
	}
	prog, ok := p.progs[sig]
	if !ok {
		return nil
	}
	return prog
}
