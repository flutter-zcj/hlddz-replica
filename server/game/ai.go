// 本文件实现机器人决策：叫分估值、主动出牌与跟牌。
// 策略目标是「不拆牌、留大牌、压队友除外」，不追求最优解，保证托管时行为合理。
package game

import "sort"

// AISuggest 为指定座位生成一手建议出牌。返回空切片表示建议过牌。
func (r *Room) AISuggest(seat int) []Card {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.player(seat)
	if p == nil || len(p.Cards) == 0 {
		return nil
	}
	if r.Last.Valid() && r.LastSeat != seat {
		// 队友已出牌且牌数很少时让牌，避免压自己人。
		if r.sameCamp(seat, r.LastSeat) && r.player(r.LastSeat).CardCount <= 2 && p.CardCount > 1 {
			return nil
		}
		if cards := r.findBeats(p.Cards, r.Last); len(cards) > 0 {
			return cards
		}
		return nil
	}
	return r.leadMove(p.Cards)
}

// sameCamp 判断两个座位是否同一阵营。
func (r *Room) sameCamp(a, b int) bool {
	if r.Landlord < 0 {
		return false
	}
	return (a == r.Landlord) == (b == r.Landlord)
}

// AIBid 估算叫分。按大牌与炸弹数量给 0~3 分。
func (r *Room) AIBid(seat int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.player(seat)
	if p == nil {
		return 0
	}
	counts := rankCount(p.Cards)
	score := 0
	for rank, c := range counts {
		switch {
		case rank == RankJokerBig:
			score += 3
		case rank == RankJokerSmall:
			score += 2
		case rank == Rank2:
			score += c
		case rank == RankA:
			score++
		}
	}
	for _, c := range counts {
		if c == 4 {
			score += 3
		}
	}
	best := r.bestBid()
	switch {
	case score >= 8:
		if best < 3 {
			return 3
		}
	case score >= 5:
		if best < 2 {
			return 2
		}
	case score >= 3:
		if best < 1 {
			return 1
		}
	}
	return 0
}

// leadMove 主动出牌：优先出最小单张或对子，保留炸弹与大牌。
func (r *Room) leadMove(hand []Card) []Card {
	counts := rankCount(hand)
	ranks := distinctRanks(counts)
	for _, rank := range ranks {
		if counts[rank] == 1 && rank < Rank2 {
			return pickRank(hand, rank, 1)
		}
	}
	for _, rank := range ranks {
		if counts[rank] == 2 && rank < Rank2 {
			return pickRank(hand, rank, 2)
		}
	}
	if m := findStraight(hand, 0, 5); len(m) > 0 {
		return m
	}
	for _, rank := range ranks {
		if counts[rank] == 3 {
			return pickRank(hand, rank, 3)
		}
	}
	return pickRank(hand, ranks[0], 1)
}

// findBeats 在手中寻找能压过 last 的最小组合，找不到则返回 nil。
func (r *Room) findBeats(hand []Card, last Pattern) []Card {
	counts := rankCount(hand)
	ranks := distinctRanks(counts)

	switch last.Type {
	case PatternSingle:
		return pickAbove(hand, counts, ranks, last.Main, 1)
	case PatternPair:
		return pickAbove(hand, counts, ranks, last.Main, 2)
	case PatternTriple:
		return pickAbove(hand, counts, ranks, last.Main, 3)
	case PatternTripleWithSingle:
		if body := pickAbove(hand, counts, ranks, last.Main, 3); len(body) > 0 {
			if kick := smallestKicker(hand, body, 1, false); len(kick) == 1 {
				return append(body, kick...)
			}
		}
	case PatternTripleWithPair:
		if body := pickAbove(hand, counts, ranks, last.Main, 3); len(body) > 0 {
			if kick := smallestKicker(hand, body, 2, true); len(kick) == 2 {
				return append(body, kick...)
			}
		}
	case PatternStraight:
		if m := findStraight(hand, last.Main, last.Len); len(m) > 0 {
			return m
		}
	case PatternStraightPair:
		if m := findStraightPair(hand, last.Main, last.Len); len(m) > 0 {
			return m
		}
	case PatternAirplane, PatternAirplaneWithSingle, PatternAirplaneWithPair:
		if m := findAirplane(hand, last); len(m) > 0 {
			return m
		}
	case PatternFourWithTwoSingle, PatternFourWithTwoPair:
		for _, rank := range ranks {
			if counts[rank] == 4 && rank > last.Main {
				body := pickRank(hand, rank, 4)
				need := last.Type == PatternFourWithTwoPair
				if kick := smallestKicker(hand, body, 2, need); len(kick) == 2 || (need && len(kick) == 4) {
					return append(body, kick...)
				}
			}
		}
	}

	// 兜底：炸弹与王炸。
	if counts[RankJokerSmall] > 0 && counts[RankJokerBig] > 0 && last.Type != PatternRocket {
		return append(pickRank(hand, RankJokerSmall, 1), pickRank(hand, RankJokerBig, 1)...)
	}
	for _, rank := range ranks {
		if counts[rank] == 4 && (last.Type != PatternBomb || rank > last.Main) {
			return pickRank(hand, rank, 4)
		}
	}
	return nil
}

// pickAbove 取点数大于 min 的最小同点牌组，张数为 n。
func pickAbove(hand []Card, counts map[int]int, ranks []int, min, n int) []Card {
	for _, rank := range ranks {
		if rank > min && counts[rank] >= n {
			return pickRank(hand, rank, n)
		}
	}
	return nil
}

// pickRank 从手牌中取指定点数的 n 张。
func pickRank(hand []Card, rank, n int) []Card {
	out := make([]Card, 0, n)
	for _, c := range hand {
		if c.Rank == rank && len(out) < n {
			out = append(out, c)
		}
	}
	return out
}

// smallestKicker 取最小余牌作为带牌。pair 为 true 时取对子。
func smallestKicker(hand, body []Card, n int, pair bool) []Card {
	rest := removeCards(hand, body)
	counts := rankCount(rest)
	ranks := distinctRanks(counts)
	out := make([]Card, 0, n)
	if pair {
		for _, rank := range ranks {
			if counts[rank] >= 2 && len(out) < n {
				out = append(out, pickRank(rest, rank, 2)...)
				if len(out) >= n {
					break
				}
			}
		}
		return out
	}
	for _, rank := range ranks {
		if len(out) >= n {
			break
		}
		out = append(out, pickRank(rest, rank, 1)...)
	}
	return out
}

// findStraight 查找长度 n、末位点数大于 min 的最小顺子。
func findStraight(hand []Card, min, n int) []Card {
	counts := rankCount(hand)
	for start := Rank3; start+n-1 <= RankA; start++ {
		end := start + n - 1
		if end <= min {
			continue
		}
		ok := true
		for rank := start; rank <= end; rank++ {
			if counts[rank] == 0 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		out := make([]Card, 0, n)
		for rank := start; rank <= end; rank++ {
			out = append(out, pickRank(hand, rank, 1)...)
		}
		return out
	}
	return nil
}

// findStraightPair 查找长度 n 对、末位点数大于 min 的最小连对。
func findStraightPair(hand []Card, min, n int) []Card {
	counts := rankCount(hand)
	for start := Rank3; start+n-1 <= RankA; start++ {
		end := start + n - 1
		if end <= min {
			continue
		}
		ok := true
		for rank := start; rank <= end; rank++ {
			if counts[rank] < 2 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		out := make([]Card, 0, n*2)
		for rank := start; rank <= end; rank++ {
			out = append(out, pickRank(hand, rank, 2)...)
		}
		return out
	}
	return nil
}

// findAirplane 查找能压过 last 的飞机组合。
func findAirplane(hand []Card, last Pattern) []Card {
	counts := rankCount(hand)
	t := last.Len
	for start := Rank3; start+t-1 <= RankA; start++ {
		end := start + t - 1
		if end <= last.Main {
			continue
		}
		ok := true
		for rank := start; rank <= end; rank++ {
			if counts[rank] < 3 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		body := make([]Card, 0, t*3)
		for rank := start; rank <= end; rank++ {
			body = append(body, pickRank(hand, rank, 3)...)
		}
		switch last.Type {
		case PatternAirplane:
			return body
		case PatternAirplaneWithSingle:
			if kick := smallestKicker(hand, body, t, false); len(kick) == t {
				return append(body, kick...)
			}
		case PatternAirplaneWithPair:
			if kick := smallestKicker(hand, body, t*2, true); len(kick) == t*2 {
				return append(body, kick...)
			}
		}
	}
	return nil
}

// SortByRankDesc 按点数降序排列，供前端手牌展示。
func SortByRankDesc(cards []Card) {
	sort.Slice(cards, func(i, j int) bool { return cards[i].Rank > cards[j].Rank })
}
