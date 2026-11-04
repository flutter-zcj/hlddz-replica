// 本文件实现斗地主牌型判定与大小比较。
// 判定采用「先纯型、后带牌型」的顺序，带牌型通过剥离主体三张/四张后校验余牌结构。
package game

// PatternType 牌型枚举。
type PatternType int

const (
	PatternInvalid            PatternType = iota
	PatternSingle                         // 单张
	PatternPair                           // 对子
	PatternTriple                         // 三张
	PatternTripleWithSingle               // 三带一
	PatternTripleWithPair                 // 三带二
	PatternStraight                       // 顺子
	PatternStraightPair                   // 连对
	PatternAirplane                       // 飞机（纯三顺）
	PatternAirplaneWithSingle             // 飞机带单
	PatternAirplaneWithPair               // 飞机带对
	PatternFourWithTwoSingle              // 四带二（两张单牌）
	PatternFourWithTwoPair                // 四带两对
	PatternBomb                           // 炸弹
	PatternRocket                         // 王炸
)

var patternNames = map[PatternType]string{
	PatternSingle:             "单张",
	PatternPair:               "对子",
	PatternTriple:             "三张",
	PatternTripleWithSingle:   "三带一",
	PatternTripleWithPair:     "三带二",
	PatternStraight:           "顺子",
	PatternStraightPair:       "连对",
	PatternAirplane:           "飞机",
	PatternAirplaneWithSingle: "飞机带单",
	PatternAirplaneWithPair:   "飞机带对",
	PatternFourWithTwoSingle:  "四带二",
	PatternFourWithTwoPair:    "四带两对",
	PatternBomb:               "炸弹",
	PatternRocket:             "王炸",
}

// String 返回牌型中文名，未知牌型返回「非法」。
func (t PatternType) String() string {
	if n, ok := patternNames[t]; ok {
		return n
	}
	return "非法"
}

// Pattern 描述一次出牌的牌型。
// Main 为比较用的主牌点数，Len 为连张组数（顺子为张数、连对为对数、飞机为三张组数）。
type Pattern struct {
	Type  PatternType `json:"type"`
	Main  int         `json:"main"`
	Len   int         `json:"len"`
	Cards []Card      `json:"cards"`
}

// Valid 判断牌型是否合法。
func (p Pattern) Valid() bool {
	return p.Type != PatternInvalid
}

// Beats 判断当前出牌能否压过 previous。
// 规则：王炸压一切；炸弹压非炸弹，炸弹之间比点数；
// 其余牌型必须同类型、同长度才可比较，且不超过 2 与双王的连张约束。
func (p Pattern) Beats(previous Pattern) bool {
	if !p.Valid() {
		return false
	}
	if !previous.Valid() {
		return true
	}
	if p.Type == PatternRocket {
		return previous.Type != PatternRocket
	}
	if previous.Type == PatternRocket {
		return false
	}
	if p.Type == PatternBomb && previous.Type != PatternBomb {
		return true
	}
	if previous.Type == PatternBomb && p.Type != PatternBomb {
		return false
	}
	if p.Type != previous.Type || p.Len != previous.Len {
		return false
	}
	return p.Main > previous.Main
}

// Parse 判定出牌组合的牌型。判定失败返回零值 Pattern 与 false。
func Parse(cards []Card) (Pattern, bool) {
	n := len(cards)
	if n == 0 {
		return Pattern{}, false
	}
	counts := rankCount(cards)

	// 王炸：双王组合，优先于对子判定（双王点数不同，不会误判为对子）。
	if n == 2 && counts[RankJokerSmall] == 1 && counts[RankJokerBig] == 1 {
		return Pattern{Type: PatternRocket, Main: RankJokerBig, Len: 1, Cards: cards}, true
	}

	if p, ok := parsePlain(cards, counts, n); ok {
		return p, true
	}
	if p, ok := parseStraightFamily(cards, counts, n); ok {
		return p, true
	}
	if p, ok := parseWithKicker(cards, counts, n); ok {
		return p, true
	}
	return Pattern{}, false
}

// parsePlain 判定单张、对子、三张、炸弹等纯同点牌型。
func parsePlain(cards []Card, counts map[int]int, n int) (Pattern, bool) {
	if !sameRank(cards) {
		return Pattern{}, false
	}
	rank := cards[0].Rank
	switch n {
	case 1:
		return Pattern{Type: PatternSingle, Main: rank, Len: 1, Cards: cards}, true
	case 2:
		// 两张王不可能同点，能在同点分支说明是普通对子。
		return Pattern{Type: PatternPair, Main: rank, Len: 1, Cards: cards}, true
	case 3:
		return Pattern{Type: PatternTriple, Main: rank, Len: 1, Cards: cards}, true
	case 4:
		return Pattern{Type: PatternBomb, Main: rank, Len: 1, Cards: cards}, true
	}
	return Pattern{}, false
}

// parseStraightFamily 判定顺子、连对与纯飞机三类连张牌型。
// 三类均不含 2 与双王，且最短长度分别为 5 张、3 对、2 组三张。
func parseStraightFamily(cards []Card, counts map[int]int, n int) (Pattern, bool) {
	ranks := distinctRanks(counts)
	if len(ranks) == 0 {
		return Pattern{}, false
	}
	maxRank := ranks[len(ranks)-1]
	if maxRank > RankA {
		return Pattern{}, false
	}
	width := n / len(ranks)
	if width*len(ranks) != n || width < 1 || width > 3 {
		return Pattern{}, false
	}
	for _, c := range counts {
		if c != width {
			return Pattern{}, false
		}
	}
	if !isConsecutive(ranks) {
		return Pattern{}, false
	}
	switch width {
	case 1:
		if len(ranks) >= 5 {
			return Pattern{Type: PatternStraight, Main: maxRank, Len: len(ranks), Cards: cards}, true
		}
	case 2:
		if len(ranks) >= 3 {
			return Pattern{Type: PatternStraightPair, Main: maxRank, Len: len(ranks), Cards: cards}, true
		}
	case 3:
		if len(ranks) >= 2 {
			return Pattern{Type: PatternAirplane, Main: maxRank, Len: len(ranks), Cards: cards}, true
		}
	}
	return Pattern{}, false
}

// parseWithKicker 判定三带一/三带二/飞机带牌/四带二等带牌型。
func parseWithKicker(cards []Card, counts map[int]int, n int) (Pattern, bool) {
	// 三带一、三带二：主体只有一组三张。
	if n == 4 || n == 5 {
		if main, ok := singleTripleRank(counts); ok {
			rest := n - 3
			if rest == 1 && countOfSingles(counts, main) == 1 && len(counts) == 2 {
				return Pattern{Type: PatternTripleWithSingle, Main: main, Len: 1, Cards: cards}, true
			}
			if rest == 2 && isPairKicker(counts, main) {
				return Pattern{Type: PatternTripleWithPair, Main: main, Len: 1, Cards: cards}, true
			}
		}
	}

	// 四带二、四带两对：主体为一组四张。
	if n == 6 || n == 8 {
		if main, ok := singleQuadRank(counts); ok {
			if kickerOK(counts, main, n-4) {
				t := PatternFourWithTwoSingle
				if n == 8 {
					t = PatternFourWithTwoPair
				}
				return Pattern{Type: t, Main: main, Len: 1, Cards: cards}, true
			}
		}
	}

	// 飞机带单、飞机带对：主体为连续多组三张。
	if p, ok := parseAirplaneWithKicker(cards, counts, n); ok {
		return p, true
	}
	return Pattern{}, false
}

// parseAirplaneWithKicker 枚举所有连续三张组，剥离主体后校验余牌张数与结构。
func parseAirplaneWithKicker(cards []Card, counts map[int]int, n int) (Pattern, bool) {
	for _, cand := range tripleRuns(counts) {
		t := len(cand)
		if t < 2 {
			continue
		}
		main := cand[t-1]
		switch n {
		case 4 * t:
			if airplaneKickerOK(counts, cand, t, false) {
				return Pattern{Type: PatternAirplaneWithSingle, Main: main, Len: t, Cards: cards}, true
			}
		case 5 * t:
			if airplaneKickerOK(counts, cand, t, true) {
				return Pattern{Type: PatternAirplaneWithPair, Main: main, Len: t, Cards: cards}, true
			}
		}
	}
	return Pattern{}, false
}

// airplaneKickerOK 校验飞机带牌结构。pairMode 为 true 时要求余牌全为对子。
func airplaneKickerOK(counts map[int]int, body []int, t int, pairMode bool) bool {
	bodySet := make(map[int]bool, len(body))
	for _, r := range body {
		bodySet[r] = true
	}
	restCount, restRanks := 0, 0
	for r, c := range counts {
		if bodySet[r] {
			c -= 3
		}
		if c <= 0 {
			continue
		}
		restCount += c
		restRanks++
		if pairMode && c != 2 {
			return false
		}
		if !pairMode && c > 2 {
			// 带单时余牌不得超过两张，超过说明对子以上结构，交给带对分支处理。
			return false
		}
	}
	if pairMode {
		return restCount == 2*t && restRanks == t
	}
	return restCount == t
}

// kickerOK 校验四带二的余牌，n 为余牌张数（2 表示两单、4 表示两对）。
func kickerOK(counts map[int]int, main int, n int) bool {
	restCount, restRanks := 0, 0
	for r, c := range counts {
		if r == main {
			c -= 4
		}
		if c <= 0 {
			continue
		}
		restCount += c
		restRanks++
	}
	if n == 2 {
		// 两单允许是两张不同牌，也允许是一个对子。
		return restCount == 2 && restRanks <= 2
	}
	if n == 4 {
		return restCount == 4 && restRanks == 2
	}
	return false
}

// singleTripleRank 返回唯一一组三张的点数。
func singleTripleRank(counts map[int]int) (int, bool) {
	found, rank := false, 0
	for r, c := range counts {
		if c == 3 {
			if found {
				return 0, false
			}
			found, rank = true, r
		}
	}
	return rank, found
}

// singleQuadRank 返回唯一一组四张的点数。
func singleQuadRank(counts map[int]int) (int, bool) {
	found, rank := false, 0
	for r, c := range counts {
		if c == 4 {
			if found {
				return 0, false
			}
			found, rank = true, r
		}
	}
	return rank, found
}

// countOfSingles 返回除主体外其余牌的总张数是否达到指定值。
func countOfSingles(counts map[int]int, main int) int {
	total := 0
	for r, c := range counts {
		if r != main {
			total += c
		}
	}
	return total
}

// isPairKicker 校验三带二的余牌是否恰好构成一个对子。
func isPairKicker(counts map[int]int, main int) bool {
	for r, c := range counts {
		if r == main {
			continue
		}
		return c == 2
	}
	return false
}

// tripleRuns 枚举所有长度不小于 2 的连续三张组，按长度与末位点数升序返回。
func tripleRuns(counts map[int]int) [][]int {
	var runs [][]int
	for start := Rank3; start <= RankA; start++ {
		if counts[start] < 3 {
			continue
		}
		run := []int{start}
		for next := start + 1; next <= RankA && counts[next] >= 3; next++ {
			run = append(run, next)
			if len(run) >= 2 {
				cp := make([]int, len(run))
				copy(cp, run)
				runs = append(runs, cp)
			}
		}
	}
	return runs
}

// distinctRanks 返回升序去重点数。
func distinctRanks(counts map[int]int) []int {
	ranks := make([]int, 0, len(counts))
	for r := range counts {
		ranks = append(ranks, r)
	}
	sortInts(ranks)
	return ranks
}

// isConsecutive 判断升序点数列是否连续。
func isConsecutive(ranks []int) bool {
	for i := 1; i < len(ranks); i++ {
		if ranks[i] != ranks[i-1]+1 {
			return false
		}
	}
	return true
}

// sortInts 对整数切片做原地升序排序。
func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
