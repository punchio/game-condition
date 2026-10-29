package condition

// regState 是注册的生命周期。
type regState uint8

const (
	regPending regState = iota + 1
	regActive
	regCanceled
)

type registration struct {
	key   InstanceKey
	plan  *plan
	h     Handler
	state regState
}

// plan 是一份表达式在某个 Manager 内的运行期绑定。
type plan struct {
	sig      string
	prog     *program
	slots    []*sourceState
	ownSlots map[*leafExpr]*sourceState
	hasEvent bool // 表达式里是否含事件叶，编译期确定

	// active 按注册顺序保存本计划的实例。注销只把 state 置为 canceled，
	// 数组本身不动，因此在回调里注销不会打断正在进行的遍历；
	// 墓碑由 sweep 在遍历之外统一清理。
	active      []*registration
	activeCount int
	dirty       bool // 有待清理的墓碑

	stamp      uint64 // 本轮是否已成为候选
	stateStamp uint64 // 本轮是否有状态依赖命中
}

// sourceState 是一个状态查询的共享求值单元：
// 指纹相同的查询（同数据域、同过滤、同聚合、同属性）只求值一次。
type sourceState struct {
	fp    string
	q     Query
	value int64
	has   bool
	dirty bool

	plans []*plan
}

// depRef 是索引条目：状态依赖带 slot（用于标脏），事件依赖只有 plan。
type depRef struct {
	plan      *plan
	slot      *sourceState
	dirtyOnly bool // 只标脏状态缓存，不唤醒计划
}

type attrKey struct {
	src  Source
	attr Attr
}

// depBucket 是某个 (域, 属性) 上的依赖集合。
type depBucket struct {
	wild  []depRef
	exact map[int64][]depRef
}

type commandKind uint8

const (
	cmdRegister commandKind = iota + 1
	cmdTrigger
)

type command struct {
	kind commandKind
	reg  *registration
	cs   *ChangeSet
}

// pendingDelta 是某个事件叶在本轮预算出的增量，与实例无关。
type pendingDelta struct {
	lp    *leafPlan
	delta int64
}

// Manager 是条件管理器，一个玩家（owner）一个，非并发安全。
// Counter 与 Plans 由业务持有并在 Manager 间共享；StateSource 绑定到该玩家。
type Manager struct {
	counter Counter
	state   StateSource
	table   *Plans

	regs    map[InstanceKey]*registration
	plans   map[string]*plan
	sources map[string]*sourceState
	deps    map[attrKey]*depBucket

	round      uint64
	queue      []command
	affected   []*plan
	dirtyPlans []*plan
	pending    []pendingDelta
	ctx        evalContext
	busy       bool
}

// NewManager 为一个玩家创建管理器。
//
// counter 与 plans 不能为空；state 可以为空（纯事件计次场景），
// 为空时状态叶与 own 恒不满足。同一份 Counter/Plans 可以跨 Manager 共享，
// 但 Counter 的键需在实例维度带上玩家信息（见 Counter 契约）。
func NewManager(counter Counter, state StateSource, plans *Plans) (*Manager, error) {
	if counter == nil {
		return nil, errNilCounter
	}
	if plans == nil {
		return nil, errNilPlans
	}
	return &Manager{
		counter: counter,
		state:   state,
		table:   plans,
		regs:    make(map[InstanceKey]*registration),
		plans:   make(map[string]*plan),
		sources: make(map[string]*sourceState),
		deps:    make(map[attrKey]*depBucket),
		ctx:     evalContext{state: state, counter: counter},
	}, nil
}

// Register 注册一个条件实例。注册后会立即求值并回调一次初始进度。
// Handler 返回 true 表示条件完成，注册随即移除并清理计数。
func (m *Manager) Register(key InstanceKey, e Expr, h Handler) error {
	if m == nil {
		return errNilManager
	}
	if e == nil {
		return errNilExpr
	}
	if m.regs[key] != nil {
		return errDuplicate
	}
	sig := e.signature()
	if m.table.get(sig) == nil {
		return errUnknownPlan
	}
	p := m.bindPlan(sig, e)
	reg := &registration{key: key, plan: p, h: h, state: regPending}
	m.regs[key] = reg
	m.submit(command{kind: cmdRegister, reg: reg})
	return nil
}

// Unregister 取消注册并清理该实例的计数；重复调用安全。
func (m *Manager) Unregister(key InstanceKey) {
	if m == nil {
		return
	}
	if reg := m.regs[key]; reg != nil {
		m.cancel(reg)
	}
}

// Len 返回当前注册的实例数（含等待执行的注册）。
func (m *Manager) Len() int {
	if m == nil {
		return 0
	}
	return len(m.regs)
}

// Trigger 提交一次业务变化；Handler 内再次调用会自动排队。
func (m *Manager) Trigger(cs *ChangeSet) {
	if m == nil || cs == nil || cs.empty() {
		return
	}
	if m.busy {
		m.queue = append(m.queue, command{kind: cmdTrigger, cs: cs.clone()})
		return
	}
	m.submit(command{kind: cmdTrigger, cs: cs})
}

// ---- 运行期绑定 ----

// bindPlan 取得计划的运行期绑定，首次使用时把状态叶挂到共享的 sourceState，
// 并把每个叶的依赖 Key 写入索引。
func (m *Manager) bindPlan(sig string, e Expr) *plan {
	if p, ok := m.plans[sig]; ok {
		return p
	}
	prog := e.plan()
	p := &plan{
		sig:      sig,
		prog:     prog,
		slots:    make([]*sourceState, prog.stateCount),
		hasEvent: prog.hasEvent(),
		active:   make([]*registration, 0, 4),
	}
	m.plans[sig] = p
	for i := range p.prog.leaves {
		lp := &p.prog.leaves[i]
		var slot *sourceState
		if lp.kind == readerState {
			slot = m.bindSource(p, lp.src)
			p.slots[lp.slot] = slot
		}
		for _, k := range lp.keys {
			m.addDep(p, k, slot)
		}
		if lp.kind == readerEvent {
			m.bindOwn(p, lp.src.own)
		}
	}
	return p
}

func (m *Manager) bindSource(p *plan, r dataReader) *sourceState {
	slot := m.sources[r.fp]
	if slot == nil {
		slot = &sourceState{fp: r.fp, q: r.q, dirty: true}
		m.sources[slot.fp] = slot
	}
	slot.plans = appendUniquePlan(slot.plans, p)
	return slot
}

func (m *Manager) bindOwn(p *plan, e exprNode) {
	switch n := e.(type) {
	case *leafExpr:
		if n.src.kind != readerState {
			return
		}
		slot := m.bindSource(p, n.src)
		if p.ownSlots == nil {
			p.ownSlots = make(map[*leafExpr]*sourceState)
		}
		p.ownSlots[n] = slot
		for _, k := range n.src.keys() {
			m.addDepMode(p, k, slot, true)
		}
	case *logicExpr:
		for _, kid := range n.children {
			m.bindOwn(p, kid)
		}
	}
}

func (m *Manager) addDep(p *plan, k ChangeKey, slot *sourceState) {
	m.addDepMode(p, k, slot, false)
}

func (m *Manager) addDepMode(p *plan, k ChangeKey, slot *sourceState, dirtyOnly bool) {
	ak := attrKey{src: k.src, attr: k.attr}
	b := m.deps[ak]
	if b == nil {
		b = &depBucket{exact: make(map[int64][]depRef, 2)}
		m.deps[ak] = b
	}
	ref := depRef{plan: p, slot: slot, dirtyOnly: dirtyOnly}
	if k.wildcard {
		b.wild = appendUniqueRef(b.wild, ref)
		return
	}
	b.exact[k.id] = appendUniqueRef(b.exact[k.id], ref)
}

func appendUniquePlan(list []*plan, p *plan) []*plan {
	for _, x := range list {
		if x == p {
			return list
		}
	}
	return append(list, p)
}

func appendUniqueRef(list []depRef, ref depRef) []depRef {
	for _, x := range list {
		if x == ref {
			return list
		}
	}
	return append(list, ref)
}

// ---- 触发调度 ----

// collect 把变更 Key 反查成候选计划，并给命中的状态查询标脏。
func (m *Manager) collect(cs *ChangeSet) {
	for _, k := range cs.keys {
		b := m.deps[attrKey{src: k.src, attr: k.attr}]
		if b == nil {
			continue
		}
		if k.wildcard {
			for _, ref := range b.wild {
				m.touch(ref)
			}
			for _, list := range b.exact {
				for _, ref := range list {
					m.touch(ref)
				}
			}
			continue
		}
		for _, ref := range b.wild {
			m.touch(ref)
		}
		for _, ref := range b.exact[k.id] {
			m.touch(ref)
		}
	}
}

func (m *Manager) touch(ref depRef) {
	if ref.slot != nil {
		ref.slot.dirty = true
		if !ref.dirtyOnly {
			ref.plan.stateStamp = m.round
		}
	}
	if ref.dirtyOnly {
		return
	}
	if ref.plan.stamp == m.round {
		return
	}
	ref.plan.stamp = m.round
	m.affected = append(m.affected, ref.plan)
}

func (m *Manager) doTrigger(cs *ChangeSet) {
	m.round++
	m.collect(cs)
	event := cs.eventID()
	for _, p := range m.affected {
		if p.activeCount == 0 {
			continue
		}
		// hasEvent 是编译期常量，先排除纯状态计划；只有含事件叶、
		// 且本轮没有状态依赖命中的计划才需要事件粗筛。
		if p.hasEvent && event != 0 && p.stateStamp != m.round && !m.eventCandidate(p, event, cs) {
			continue
		}
		m.collectDelta(p, event, cs)
		// 直接按注册顺序遍历：回调内注销只改 state，不会移动元素。
		for _, reg := range p.active {
			if reg.state != regActive {
				continue
			}
			m.applyCount(reg)
			m.fire(reg)
		}
	}
	m.affected = m.affected[:0]
}

// eventCandidate 报告计划里是否至少一个事件叶可能被本次事件命中。
func (m *Manager) eventCandidate(p *plan, event int64, cs *ChangeSet) bool {
	for i := range p.prog.leaves {
		if p.prog.leaves[i].src.mayMatch(event, cs) {
			return true
		}
	}
	return false
}

// collectDelta 预算本轮事件增量：过滤与聚合只依赖计划与事件，
// 与实例无关，因此每个计划每轮只算一次。
func (m *Manager) collectDelta(p *plan, event int64, cs *ChangeSet) {
	m.pending = m.pending[:0]
	if event == 0 {
		return
	}
	for i := range p.prog.leaves {
		lp := &p.prog.leaves[i]
		if !lp.src.mayMatch(event, cs) {
			continue
		}
		delta, has := lp.src.delta(lp.acc, cs.records)
		if !has {
			continue
		}
		m.pending = append(m.pending, pendingDelta{lp: lp, delta: delta})
	}
}

// applyCount 把预算好的增量记到该实例上；own 按实例检查，状态查询使用轮内共享缓存。
func (m *Manager) applyCount(reg *registration) {
	m.ctx.key = reg.key
	m.ctx.plan = reg.plan
	for i := range m.pending {
		pd := &m.pending[i]
		if !pd.lp.src.allow(&m.ctx) {
			continue
		}
		m.counter.Update(reg.key, pd.lp.node, pd.lp.acc, pd.delta)
	}
}

// fire 求值一个实例并回调；每次求值使用独立进度切片。
func (m *Manager) fire(reg *registration) {
	p := reg.plan
	m.ctx.key = reg.key
	m.ctx.plan = p
	match, progress := evalPlan(p, &m.ctx)
	res := Result{Round: m.round, Progress: progress, Match: match}
	if h := reg.h; h != nil && h(res) {
		m.cancel(reg)
	}
}

// ---- 注册生命周期 ----

func (m *Manager) submit(c command) {
	m.queue = append(m.queue, c)
	if m.busy {
		return
	}
	m.run()
}

func (m *Manager) run() {
	m.busy = true
	defer func() { m.busy = false }()
	for i := 0; i < len(m.queue); i++ {
		c := m.queue[i]
		m.queue[i] = command{}
		switch c.kind {
		case cmdRegister:
			m.doRegister(c.reg)
		case cmdTrigger:
			m.doTrigger(c.cs)
		}
	}
	m.queue = m.queue[:0]
	// 本轮遍历全部结束，统一回收墓碑。
	m.sweepDirty()
}

func (m *Manager) doRegister(reg *registration) {
	if reg.state != regPending {
		return
	}
	p := reg.plan
	reg.state = regActive
	p.active = append(p.active, reg)
	p.activeCount++
	m.fire(reg)
}

// cancel 只把实例标记为墓碑并摘掉外部索引，物理回收交给 sweep：
// 这样回调内注销任意实例都不会打断本轮遍历，也不需要 nil 空洞。
func (m *Manager) cancel(reg *registration) {
	if reg.state == regCanceled {
		return
	}
	if reg.state == regActive {
		p := reg.plan
		p.activeCount--
		if !p.dirty {
			p.dirty = true
			m.dirtyPlans = append(m.dirtyPlans, p)
		}
	}
	reg.state = regCanceled
	if m.regs[reg.key] == reg {
		delete(m.regs, reg.key)
	}
	m.counter.Del(reg.key)
	// 不在触发过程中时立即回收，避免墓碑跨轮残留。
	if !m.busy {
		m.sweepDirty()
	}
}

// sweepDirty 清理所有被标记的计划。只能在触发遍历之外调用：
// run 的结尾，或非 busy 的注销路径。
func (m *Manager) sweepDirty() {
	for _, p := range m.dirtyPlans {
		p.sweep()
	}
	m.dirtyPlans = m.dirtyPlans[:0]
}

// sweep 移除墓碑并保持原有注册顺序；数组长度即活跃实例数。
func (p *plan) sweep() {
	if !p.dirty {
		return
	}
	out := p.active[:0]
	for _, reg := range p.active {
		if reg.state == regCanceled {
			continue
		}
		out = append(out, reg)
	}
	clear(p.active[len(out):])
	p.active = out
	p.activeCount = len(out)
	p.dirty = false
}
