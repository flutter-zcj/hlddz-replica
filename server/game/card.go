// Package game 实现经典三人斗地主的牌局核心逻辑。
// 本文件定义牌、牌堆与牌面文案，判型逻辑见 pattern.go。
package game

import (
	"fmt"
	"sort"
)

// 牌面点数取值。3~15 对应 3~2，16 为小王，17 为大王。
// 采用连续取值便于顺子与连对的连续性判断，2 与双王不参与连张。
const (
	Rank3          = 3
	RankA          = 14
	Rank2          = 15
	RankJokerSmall = 16
	RankJokerBig   = 17
)

// 花色取值，王的花色固定为 SuitNone。
const (
	SuitDiamond = 0
	SuitClub    = 1
	SuitHeart   = 2
	SuitSpade   = 3
	SuitNone    = 4
)

// Card 表示一张牌。
type Card struct {
	Suit int `json:"suit"`
	Rank int `json:"rank"`
}

// IsJoker 判断是否为大小王。
func (c Card) IsJoker() bool {
	return c.Rank == RankJokerSmall || c.Rank == RankJokerBig
}

// String 返回牌面文案，用于日志与调试，例如 "♠A"、"大王"。
func (c Card) String() string {
	if c.Rank == RankJokerBig {
		return "大王"
	}
	if c.Rank == RankJokerSmall {
		return "小王"
	}
	suits := [...]string{"♦", "♣", "♥", "♠"}
	names := map[int]string{
		11: "J", 12: "Q", 13: "K", 14: "A", 15: "2",
	}
	if n, ok := names[c.Rank]; ok {
		return suits[c.Suit] + n
	}
	return fmt.Sprintf("%s%d", suits[c.Suit], c.Rank)
}

// NewDeck 生成一副 54 张牌，顺序为 3~2 的四门花色，随后小王、大王。
func NewDeck() []Card {
	deck := make([]Card, 0, 54)
	for rank := Rank3; rank <= Rank2; rank++ {
		for suit := SuitDiamond; suit <= SuitSpade; suit++ {
			deck = append(deck, Card{Suit: suit, Rank: rank})
		}
	}
	deck = append(deck, Card{Suit: SuitNone, Rank: RankJokerSmall})
	deck = append(deck, Card{Suit: SuitNone, Rank: RankJokerBig})
	return deck
}

// SortCards 按点数升序排列，同点数按花色升序，便于前端稳定渲染。
func SortCards(cards []Card) {
	sort.Slice(cards, func(i, j int) bool {
		if cards[i].Rank != cards[j].Rank {
			return cards[i].Rank < cards[j].Rank
		}
		return cards[i].Suit < cards[j].Suit
	})
}

// rankCount 返回点数到张数的计数表。
func rankCount(cards []Card) map[int]int {
	m := make(map[int]int, len(cards))
	for _, c := range cards {
		m[c.Rank]++
	}
	return m
}

// sameRank 判断所有牌是否同点数。
func sameRank(cards []Card) bool {
	if len(cards) == 0 {
		return false
	}
	for _, c := range cards[1:] {
		if c.Rank != cards[0].Rank {
			return false
		}
	}
	return true
}
