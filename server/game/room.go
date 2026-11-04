// 本文件实现房间与牌局状态机：发牌、叫分、加倍、出牌、结算。
// 状态流转严格单向，任何越阶段的操作都会被拒绝。
package game

import (
	"errors"
	"math/rand"
	"sync"
)

// Phase 牌局阶段。
type Phase int

const (
	PhaseWaiting  Phase = iota // 等待玩家准备
	PhaseBidding               // 叫地主
	PhaseDoubling              // 加倍
	PhasePlaying               // 出牌
	PhaseSettled               // 已结算
)

var phaseNames = map[Phase]string{
	PhaseWaiting:  "等待中",
	PhaseBidding:  "叫地主",
	PhaseDoubling: "加倍",
	PhasePlaying:  "出牌",
	PhaseSettled:  "已结算",
}

// String 返回阶段中文名。
func (p Phase) String() string {
	if n, ok := phaseNames[p]; ok {
		return n
	}
	return "未知"
}

// 业务错误定义，供上层映射为错误码。
var (
	ErrSeatOccupied   = errors.New("座位已被占用")
	ErrNotYourTurn    = errors.New("未轮到你操作")
	ErrPhaseMismatch  = errors.New("当前阶段不允许该操作")
	ErrCardsNotOwned  = errors.New("手牌中没有这些牌")
	ErrPatternInvalid = errors.New("牌型不合法")
	ErrCannotBeat     = errors.New("压不过上家的牌")
	ErrMustPlay       = errors.New("本轮必须出牌")
	ErrRoomFull       = errors.New("房间已满")
)

// Player 房间内的玩家。
type Player struct {
	ID         string `json:"id"`
	Nickname   string `json:"nickname"`
	Seat       int    `json:"seat"`
	Ready      bool   `json:"ready"`
	Online     bool   `json:"online"`
	IsRobot    bool   `json:"is_robot"`
	IsLandlord bool   `json:"is_landlord"`
	Cards      []Card `json:"cards,omitempty"`
	CardCount  int    `json:"card_count"`
	BidScore   int    `json:"bid_score"`
	Doubled    bool   `json:"doubled"`
	DoubleDone bool   `json:"double_done"`
	Played     int    `json:"played"`
	Score      int    `json:"score"`
}

// Room 一个三人牌桌。
type Room struct {
	mu sync.Mutex

	ID         string     `json:"id"`
	Phase      Phase      `json:"phase"`
	BaseScore  int        `json:"base_score"`
	Players    [3]*Player `json:"players"`
	Bottom     []Card     `json:"bottom"`
	Landlord   int        `json:"landlord"`
	Current    int        `json:"current"`
	LastSeat   int        `json:"last_seat"`
	Last       Pattern    `json:"last"`
	BidTurn    int        `json:"bid_turn"`
	BidCount   int        `json:"bid_count"`
	Bomb       int        `json:"bomb"`
	Multiplier int        `json:"multiplier"`
	Round      int        `json:"round"`
	rng        *rand.Rand
}

// NewRoom 创建房间，baseScore 为底分。
func NewRoom(id string, baseScore int) *Room {
	if baseScore <= 0 {
		baseScore = 100
	}
	return &Room{
		ID:        id,
		Phase:     PhaseWaiting,
		BaseScore: baseScore,
		Landlord:  -1,
		LastSeat:  -1,
		rng:       rand.New(rand.NewSource(rand.Int63())),
	}
}

// Lock 与 Unlock 供上层在需要原子读取时使用。
func (r *Room) Lock()   { r.mu.Lock() }
func (r *Room) Unlock() { r.mu.Unlock() }

// Reset 清空房间，准备新一局。用于一局结束后复用房间对象。
func (r *Room) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.Players {
		r.Players[i] = nil
	}
	r.Phase = PhaseWaiting
	r.Bottom = nil
	r.Landlord = -1
	r.LastSeat = -1
	r.Last = Pattern{}
	r.Bomb = 0
	r.Multiplier = 1
	r.BidCount = 0
	r.Current = 0
	r.BidTurn = 0
}

// Join 让玩家入座，返回座位号。机器人不占用真人座位上限。
func (r *Room) Join(id, nickname string, robot bool) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for seat := 0; seat < 3; seat++ {
		if r.Players[seat] == nil {
			r.Players[seat] = &Player{ID: id, Nickname: nickname, Seat: seat, Online: true, IsRobot: robot}
			return seat, nil
		}
	}
	return -1, ErrRoomFull
}

// Leave 玩家离开座位，牌局进行中不允许离开。
func (r *Room) Leave(seat int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Phase != PhaseWaiting && r.Phase != PhaseSettled {
		return ErrPhaseMismatch
	}
	if seat < 0 || seat >= 3 {
		return ErrRoomFull
	}
	r.Players[seat] = nil
	return nil
}

// Ready 设置准备状态，三人全部准备后自动发牌。
func (r *Room) Ready(seat int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.player(seat)
	if p == nil {
		return ErrRoomFull
	}
	p.Ready = true
	if r.allReady() {
		r.dealLocked()
	}
	return nil
}

// allReady 判断是否三人均已准备。
func (r *Room) allReady() bool {
	for _, p := range r.Players {
		if p == nil || !p.Ready {
			return false
		}
	}
	return true
}

// player 安全取座位上的玩家。
func (r *Room) player(seat int) *Player {
	if seat < 0 || seat >= 3 {
		return nil
	}
	return r.Players[seat]
}

// dealLocked 洗牌并发牌，随后进入叫分阶段。调用方需持锁。
func (r *Room) dealLocked() {
	deck := NewDeck()
	r.rng.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	for seat := 0; seat < 3; seat++ {
		p := r.Players[seat]
		p.Cards = make([]Card, 17)
		copy(p.Cards, deck[seat*17:seat*17+17])
		SortCards(p.Cards)
		p.CardCount = 17
		p.IsLandlord = false
		p.BidScore = 0
		p.Doubled = false
		p.Played = 0
		p.Ready = false
	}
	r.Bottom = make([]Card, 3)
	copy(r.Bottom, deck[51:])
	SortCards(r.Bottom)
	r.Phase = PhaseBidding
	r.Landlord = -1
	r.Last = Pattern{}
	r.LastSeat = -1
	r.Bomb = 0
	r.Multiplier = 1
	r.BidCount = 0
	r.BidTurn = r.rng.Intn(3)
	r.Current = r.BidTurn
	r.Round++
}

// Bid 叫分。score 取 0（不叫）或 1~3，必须高于当前最高分。
func (r *Room) Bid(seat, score int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Phase != PhaseBidding {
		return ErrPhaseMismatch
	}
	if seat != r.BidTurn {
		return ErrNotYourTurn
	}
	if score < 0 || score > 3 {
		return ErrPatternInvalid
	}
	best := r.bestBid()
	if score != 0 && score <= best {
		return ErrPatternInvalid
	}
	p := r.player(seat)
	p.BidScore = score
	r.BidCount++

	if score == 3 {
		r.finishBidding(seat)
		return nil
	}
	if r.BidCount >= 3 {
		winner := r.highestBidder()
		if winner < 0 {
			// 三人均不叫，重新发牌。
			r.dealLocked()
			return nil
		}
		r.finishBidding(winner)
		return nil
	}
	r.BidTurn = (r.BidTurn + 1) % 3
	r.Current = r.BidTurn
	return nil
}

// bestBid 返回当前最高叫分。
func (r *Room) bestBid() int {
	best := 0
	for _, p := range r.Players {
		if p != nil && p.BidScore > best {
			best = p.BidScore
		}
	}
	return best
}

// highestBidder 返回叫分最高的座位，无人叫分返回 -1。
func (r *Room) highestBidder() int {
	best, seat := 0, -1
	for i, p := range r.Players {
		if p != nil && p.BidScore > best {
			best, seat = p.BidScore, i
		}
	}
	return seat
}

// finishBidding 确定地主，发底牌并进入加倍阶段。
func (r *Room) finishBidding(seat int) {
	p := r.player(seat)
	if p == nil {
		return
	}
	r.Landlord = seat
	p.IsLandlord = true
	p.Cards = append(p.Cards, r.Bottom...)
	SortCards(p.Cards)
	p.CardCount = len(p.Cards)
	r.Multiplier = p.BidScore
	if r.Multiplier <= 0 {
		r.Multiplier = 1
	}
	r.Phase = PhaseDoubling
	r.Current = r.Landlord
}

// Double 加倍。doubled 为 true 时本局该玩家倍数翻倍。
func (r *Room) Double(seat int, doubled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Phase != PhaseDoubling {
		return ErrPhaseMismatch
	}
	p := r.player(seat)
	if p == nil {
		return ErrRoomFull
	}
	if p.DoubleDone {
		return ErrPhaseMismatch
	}
	p.Doubled = doubled
	p.DoubleDone = true
	if r.allDoubled() {
		r.startPlayingLocked()
	}
	return nil
}

// allDoubled 判断三位玩家是否都已完成加倍选择。
func (r *Room) allDoubled() bool {
	for _, p := range r.Players {
		if p == nil || !p.DoubleDone {
			return false
		}
	}
	return true
}

// startPlayingLocked 进入出牌阶段，地主先出，桌面无待压牌。
func (r *Room) startPlayingLocked() {
	r.Phase = PhasePlaying
	r.Current = r.Landlord
	r.Last = Pattern{}
	r.LastSeat = -1
}

// Pass 过牌。新一轮首家不可过。
func (r *Room) Pass(seat int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Phase != PhasePlaying {
		return ErrPhaseMismatch
	}
	if seat != r.Current {
		return ErrNotYourTurn
	}
	if !r.Last.Valid() || r.LastSeat == seat {
		return ErrMustPlay
	}
	r.advance()
	return nil
}

// Play 出牌。cards 必须是该玩家手牌的子集，且牌型合法并能压过桌面。
func (r *Room) Play(seat int, cards []Card) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Phase != PhasePlaying {
		return ErrPhaseMismatch
	}
	if seat != r.Current {
		return ErrNotYourTurn
	}
	p := r.player(seat)
	if p == nil {
		return ErrRoomFull
	}
	if !containsAll(p.Cards, cards) {
		return ErrCardsNotOwned
	}
	pat, ok := Parse(cards)
	if !ok {
		return ErrPatternInvalid
	}
	if r.Last.Valid() && r.LastSeat != seat && !pat.Beats(r.Last) {
		return ErrCannotBeat
	}
	p.Cards = removeCards(p.Cards, cards)
	p.CardCount = len(p.Cards)
	p.Played++
	if pat.Type == PatternBomb || pat.Type == PatternRocket {
		r.Bomb++
	}
	r.Last = pat
	r.LastSeat = seat
	if len(p.Cards) == 0 {
		r.settleLocked(seat)
		return nil
	}
	r.advance()
	return nil
}

// advance 轮转到下一位；若回到最后出牌者则清空桌面牌。
func (r *Room) advance() {
	r.Current = (r.Current + 1) % 3
	if r.Current == r.LastSeat {
		r.Last = Pattern{}
	}
}

// settleLocked 判定胜负并计算分数。winner 为走完手牌的座位。
func (r *Room) settleLocked(winner int) {
	r.Phase = PhaseSettled
	landlordWin := winner == r.Landlord
	spring := r.isSpring(landlordWin)

	mult := r.Multiplier
	for i := 0; i < r.Bomb; i++ {
		mult *= 2
	}
	doubles := 0
	for _, p := range r.Players {
		if p != nil && p.Doubled {
			doubles++
		}
	}
	for i := 0; i < doubles; i++ {
		mult *= 2
	}
	if spring {
		mult *= 2
	}
	unit := r.BaseScore * mult

	for i, p := range r.Players {
		if p == nil {
			continue
		}
		switch {
		case i == r.Landlord && landlordWin:
			p.Score = 2 * unit
		case i == r.Landlord && !landlordWin:
			p.Score = -2 * unit
		case i != r.Landlord && landlordWin:
			p.Score = -unit
		default:
			p.Score = unit
		}
	}
	r.Multiplier = mult
}

// isSpring 判断春天：地主赢且农民一张未出，或农民赢且地主只出过一手。
func (r *Room) isSpring(landlordWin bool) bool {
	if landlordWin {
		for i, p := range r.Players {
			if p == nil || i == r.Landlord {
				continue
			}
			if p.Played > 0 {
				return false
			}
		}
		return true
	}
	lp := r.player(r.Landlord)
	return lp != nil && lp.Played <= 1
}

// containsAll 判断手牌是否包含指定的全部牌（按牌面多重集比较）。
func containsAll(hand, cards []Card) bool {
	if len(cards) == 0 {
		return false
	}
	bucket := make(map[Card]int, len(hand))
	for _, c := range hand {
		bucket[c]++
	}
	for _, c := range cards {
		if bucket[c] == 0 {
			return false
		}
		bucket[c]--
	}
	return true
}

// removeCards 从手牌中移除指定牌。
func removeCards(hand, cards []Card) []Card {
	bucket := make(map[Card]int, len(cards))
	for _, c := range cards {
		bucket[c]++
	}
	out := make([]Card, 0, len(hand))
	for _, c := range hand {
		if bucket[c] > 0 {
			bucket[c]--
			continue
		}
		out = append(out, c)
	}
	return out
}
