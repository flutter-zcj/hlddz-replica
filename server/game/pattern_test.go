// 本文件覆盖牌型判定与比较的关键分支，含规则边界。
package game

import "testing"

// cards 按点数构造测试牌，花色按序轮转，避免同名牌冲突。
func cards(ranks ...int) []Card {
	suit := 0
	out := make([]Card, 0, len(ranks))
	used := make(map[int]int)
	for _, r := range ranks {
		if r >= RankJokerSmall {
			out = append(out, Card{Suit: SuitNone, Rank: r})
			continue
		}
		used[r]++
		suit = used[r] - 1
		out = append(out, Card{Suit: suit, Rank: r})
	}
	return out
}

func TestParseBasic(t *testing.T) {
	cases := []struct {
		name string
		in   []Card
		want PatternType
		main int
		len  int
	}{
		{"单张", cards(5), PatternSingle, 5, 1},
		{"对子", cards(5, 5), PatternPair, 5, 1},
		{"三张", cards(7, 7, 7), PatternTriple, 7, 1},
		{"炸弹", cards(9, 9, 9, 9), PatternBomb, 9, 1},
		{"王炸", cards(RankJokerSmall, RankJokerBig), PatternRocket, RankJokerBig, 1},
		{"三带一", cards(7, 7, 7, 3), PatternTripleWithSingle, 7, 1},
		{"三带二", cards(7, 7, 7, 3, 3), PatternTripleWithPair, 7, 1},
		{"顺子5张", cards(3, 4, 5, 6, 7), PatternStraight, 7, 5},
		{"顺子到A", cards(10, 11, 12, 13, 14), PatternStraight, 14, 5},
		{"连对3对", cards(3, 3, 4, 4, 5, 5), PatternStraightPair, 5, 3},
		{"纯飞机", cards(3, 3, 3, 4, 4, 4), PatternAirplane, 4, 2},
		{"飞机带单", cards(3, 3, 3, 4, 4, 4, 6, 7), PatternAirplaneWithSingle, 4, 2},
		{"飞机带对", cards(3, 3, 3, 4, 4, 4, 6, 6, 7, 7), PatternAirplaneWithPair, 4, 2},
		{"四带二", cards(9, 9, 9, 9, 3, 5), PatternFourWithTwoSingle, 9, 1},
		{"四带两对", cards(9, 9, 9, 9, 3, 3, 5, 5), PatternFourWithTwoPair, 9, 1},
	}
	for _, c := range cases {
		p, ok := Parse(c.in)
		if !ok {
			t.Errorf("%s: 判定失败", c.name)
			continue
		}
		if p.Type != c.want {
			t.Errorf("%s: 牌型 = %v，期望 %v", c.name, p.Type, c.want)
		}
		if p.Main != c.main {
			t.Errorf("%s: 主牌 = %d，期望 %d", c.name, p.Main, c.main)
		}
		if p.Len != c.len {
			t.Errorf("%s: 长度 = %d，期望 %d", c.name, p.Len, c.len)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name string
		in   []Card
	}{
		{"4张顺子太短", cards(3, 4, 5, 6)},
		{"顺子含2", cards(11, 12, 13, 14, 15)},
		{"顺子含王", cards(13, 14, RankJokerSmall, 3, 4)},
		{"连对仅2对", cards(3, 3, 4, 4)},
		{"连对含2", cards(13, 13, 14, 14, 15, 15)},
		{"飞机不连续", cards(3, 3, 3, 5, 5, 5)},
		{"三带二用两单", cards(7, 7, 7, 3, 5)},
		{"飞机带单数量不符", cards(3, 3, 3, 4, 4, 4, 6)},
		{"空牌", []Card{}},
	}
	for _, c := range cases {
		if p, ok := Parse(c.in); ok {
			t.Errorf("%s: 期望非法，实际判定为 %v", c.name, p.Type)
		}
	}
}

func TestBeats(t *testing.T) {
	mustParse := func(in []Card) Pattern {
		p, ok := Parse(in)
		if !ok {
			t.Fatalf("构造牌型失败: %v", in)
		}
		return p
	}
	// 同型比点数
	if !mustParse(cards(9)).Beats(mustParse(cards(8))) {
		t.Error("单张 9 应压过 8")
	}
	if mustParse(cards(8)).Beats(mustParse(cards(9))) {
		t.Error("单张 8 不应压过 9")
	}
	// 炸弹压普通牌型
	if !mustParse(cards(3, 3, 3, 3)).Beats(mustParse(cards(2))) {
		t.Error("炸弹应压过单张 2")
	}
	// 王炸压炸弹
	if !mustParse(cards(RankJokerSmall, RankJokerBig)).Beats(mustParse(cards(15, 15, 15, 15))) {
		t.Error("王炸应压过炸弹")
	}
	// 炸弹之间比点数
	if !mustParse(cards(10, 10, 10, 10)).Beats(mustParse(cards(9, 9, 9, 9))) {
		t.Error("10 炸弹应压过 9 炸弹")
	}
	// 不同牌型不可比较
	if mustParse(cards(9, 9)).Beats(mustParse(cards(8))) {
		t.Error("对子不应压过单张")
	}
	// 长度不同的顺子不可比较
	if mustParse(cards(4, 5, 6, 7, 8, 9)).Beats(mustParse(cards(3, 4, 5, 6, 7))) {
		t.Error("6 张顺子不应压过 5 张顺子")
	}
	// 首家出牌
	var empty Pattern
	if !mustParse(cards(3)).Beats(empty) {
		t.Error("首家出牌应视为可出")
	}
}

func TestNewDeck(t *testing.T) {
	deck := NewDeck()
	if len(deck) != 54 {
		t.Fatalf("牌堆张数 = %d，期望 54", len(deck))
	}
	seen := make(map[Card]bool)
	for _, c := range deck {
		if seen[c] {
			t.Fatalf("重复的牌: %v", c)
		}
		seen[c] = true
	}
}
