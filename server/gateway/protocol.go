// Package gateway 提供斗地主牌桌的 WebSocket 接入层。
// 协议为单帧 JSON：{"cmd":"play","data":{...}}，服务端以同结构回推状态。
package gateway

import (
	"encoding/json"
	"sync"

	"ddz/game"
)

// Command 客户端指令名。
const (
	CmdLogin  = "login"
	CmdJoin   = "join"
	CmdReady  = "ready"
	CmdBid    = "bid"
	CmdDouble = "double"
	CmdPlay   = "play"
	CmdPass   = "pass"
	CmdState  = "state"
	CmdError  = "error"
	CmdEnter  = "enter"
)

// Request 客户端请求体。
type Request struct {
	Cmd  string          `json:"cmd"`
	Data json.RawMessage `json:"data"`
}

// Response 服务端下行消息。
type Response struct {
	Cmd  string `json:"cmd"`
	Data any    `json:"data,omitempty"`
}

// LoginData 登录请求参数。
type LoginData struct {
	Token    string `json:"token"`
	Nickname string `json:"nickname"`
}

// BidData 叫分请求参数。
type BidData struct {
	Score int `json:"score"`
}

// DoubleData 加倍请求参数。
type DoubleData struct {
	Double bool `json:"double"`
}

// PlayData 出牌请求参数。
type PlayData struct {
	Cards []game.Card `json:"cards"`
}

// ErrorData 错误消息。
type ErrorData struct {
	Message string `json:"message"`
}

// StateData 牌桌状态快照，按请求者视角裁剪手牌。
type StateData struct {
	RoomID     string        `json:"room_id"`
	Phase      string        `json:"phase"`
	PhaseCode  int           `json:"phase_code"`
	BaseScore  int           `json:"base_score"`
	Multiplier int           `json:"multiplier"`
	Bomb       int           `json:"bomb"`
	Landlord   int           `json:"landlord"`
	Current    int           `json:"current"`
	LastSeat   int           `json:"last_seat"`
	LastType   string        `json:"last_type"`
	LastMain   int           `json:"last_main"`
	LastLen    int           `json:"last_len"`
	LastCards  []game.Card   `json:"last_cards"`
	Bottom     []game.Card   `json:"bottom"`
	MySeat     int           `json:"my_seat"`
	MyCards    []game.Card   `json:"my_cards"`
	Players    []PlayerState `json:"players"`
	YourTurn   bool          `json:"your_turn"`
	CanPass    bool          `json:"can_pass"`
}

// PlayerState 单个玩家的公开信息。
type PlayerState struct {
	Seat       int    `json:"seat"`
	Nickname   string `json:"nickname"`
	CardCount  int    `json:"card_count"`
	IsLandlord bool   `json:"is_landlord"`
	IsRobot    bool   `json:"is_robot"`
	Online     bool   `json:"online"`
	Score      int    `json:"score"`
	BidScore   int    `json:"bid_score"`
	Doubled    bool   `json:"doubled"`
	Ready      bool   `json:"ready"`
}

// session 一个客户端连接。
type session struct {
	mu   sync.Mutex
	id   string
	seat int
	send func([]byte) error
}

// Send 线程安全地下发消息。
func (s *session) Send(resp Response) {
	b, err := json.Marshal(resp)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.send(b)
}

// SendError 下发错误信息。
func (s *session) SendError(msg string) {
	s.Send(Response{Cmd: CmdError, Data: ErrorData{Message: msg}})
}

// BuildState 按视角生成状态快照。viewer 为观察者座位，-1 表示旁观。
func BuildState(r *game.Room, viewer int) StateData {
	r.Lock()
	defer r.Unlock()

	st := StateData{
		RoomID:     r.ID,
		Phase:      r.Phase.String(),
		PhaseCode:  int(r.Phase),
		BaseScore:  r.BaseScore,
		Multiplier: r.Multiplier,
		Bomb:       r.Bomb,
		Landlord:   r.Landlord,
		Current:    r.Current,
		LastSeat:   r.LastSeat,
		LastType:   r.Last.Type.String(),
		LastMain:   r.Last.Main,
		LastLen:    r.Last.Len,
		LastCards:  r.Last.Cards,
		Bottom:     r.Bottom,
		MySeat:     viewer,
	}
	for i, p := range r.Players {
		if p == nil {
			st.Players = append(st.Players, PlayerState{Seat: i})
			continue
		}
		st.Players = append(st.Players, PlayerState{
			Seat:       i,
			Nickname:   p.Nickname,
			CardCount:  p.CardCount,
			IsLandlord: p.IsLandlord,
			IsRobot:    p.IsRobot,
			Online:     p.Online,
			Score:      p.Score,
			BidScore:   p.BidScore,
			Doubled:    p.Doubled,
			Ready:      p.Ready,
		})
		if i == viewer {
			st.MyCards = append([]game.Card{}, p.Cards...)
		}
	}
	st.YourTurn = viewer == r.Current
	st.CanPass = r.Last.Valid() && r.LastSeat != viewer
	return st
}
