package condition

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// cmpOp 是归一化后的比较算子：配置里的 <、<=、>、>= 在入口折叠成闭区间。
type cmpOp uint8

const (
	cmpEqual cmpOp = iota
	cmpNotEqual
	cmpInRange
)

func (c cmpOp) String() string {
	switch c {
	case cmpEqual:
		return "="
	case cmpNotEqual:
		return "!="
	case cmpInRange:
		return "range"
	default:
		return "?"
	}
}

func cmpText(c cmpOp, values []int64) string {
	switch c {
	case cmpEqual:
		return "=" + joinInts(values)
	case cmpNotEqual:
		return "!=" + joinInts(values)
	case cmpInRange:
		if len(values) != 2 {
			return "?"
		}
		lo, hi := values[0], values[1]
		switch {
		case hi == math.MaxInt64:
			return ">=" + strconv.FormatInt(lo, 10)
		case lo == math.MinInt64:
			return "<=" + strconv.FormatInt(hi, 10)
		}
		return "[" + strconv.FormatInt(lo, 10) + "," + strconv.FormatInt(hi, 10) + "]"
	}
	return "?"
}

func joinInts(values []int64) string {
	var b strings.Builder
	for i, v := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(v, 10))
	}
	return b.String()
}

// parseCmp 把配置层算子归一化为运行期算子与比较值。
// 入参 values 不得为 MinInt64 / MaxInt64，这两个值保留给无界端点。
func parseCmp(op string, values ...int64) (cmpOp, []int64, error) {
	for i, v := range values {
		if v == math.MinInt64 || v == math.MaxInt64 {
			return 0, nil, fmt.Errorf("比较值 #%d 不能是 MinInt64/MaxInt64", i)
		}
	}
	switch op {
	case "=":
		set := sortedUnique(values)
		if len(set) == 0 {
			return 0, nil, fmt.Errorf("算子 %q 至少需要一个值", op)
		}
		return cmpEqual, set, nil
	case "!=":
		set := sortedUnique(values)
		if len(set) == 0 {
			return 0, nil, fmt.Errorf("算子 %q 至少需要一个值", op)
		}
		return cmpNotEqual, set, nil
	case "range":
		if len(values) != 2 {
			return 0, nil, fmt.Errorf("区间需要两个端点，得到 %d 个", len(values))
		}
		out := []int64{values[0], values[1]}
		slices.Sort(out)
		return cmpInRange, out, nil
	case "<", "<=", ">", ">=":
		if len(values) != 1 {
			return 0, nil, fmt.Errorf("算子 %q 只能有一个值，得到 %d 个", op, len(values))
		}
		lo, hi := boundCmp(op, values[0])
		return cmpInRange, []int64{lo, hi}, nil
	default:
		return 0, nil, fmt.Errorf("未知算子 %q", op)
	}
}

// boundCmp 把 <、<=、>、>= 折叠成闭区间端点，无界一侧用哨兵表示。
func boundCmp(op string, v int64) (int64, int64) {
	switch op {
	case "<":
		return math.MinInt64, v - 1
	case "<=":
		return math.MinInt64, v
	case ">":
		return v + 1, math.MaxInt64
	default:
		return v, math.MaxInt64
	}
}

func sortedUnique(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	out := append([]int64(nil), values...)
	slices.Sort(out)
	return slices.Compact(out)
}

// predicate 按归一化算子判定；无值确定不满足。
func predicate(cmp cmpOp, values []int64, value int64, has bool) bool {
	if !has {
		return false
	}
	switch cmp {
	case cmpEqual:
		for _, want := range values {
			if value == want {
				return true
			}
		}
		return false
	case cmpNotEqual:
		for _, want := range values {
			if value == want {
				return false
			}
		}
		return true
	case cmpInRange:
		return len(values) == 2 && values[0] <= value && value <= values[1]
	default:
		return false
	}
}

// validateCmp 校验归一化后的算子与比较值形态匹配。
func validateCmp(cmp cmpOp, values []int64) error {
	switch cmp {
	case cmpInRange:
		if len(values) != 2 {
			return fmt.Errorf("区间比较需要恰好 2 个端点，得到 %d 个", len(values))
		}
	case cmpEqual, cmpNotEqual:
		if len(values) == 0 {
			return fmt.Errorf("算子 %q 至少需要一个比较值", cmp)
		}
	default:
		return fmt.Errorf("非法比较算子 %d", uint8(cmp))
	}
	return nil
}
