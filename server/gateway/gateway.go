// 本文件实现 WebSocket 网关组件：连接管理、单房间调度与机器人驱动。
// 所有房间操作经 Hub 串行执行，避免并发修改牌局状态。
package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	cfacade "github.com/cherry-game/cherry/facade"
	cherryConnector "github.com/cherry-game/cherry/net/connector"

	"ddz/game"
)

// robotDelay 机器人思考延时，过长影响节奏，过短显得突兀。
const robotDelay = 900 * time.Millisecond

// Gateway 是 cherry 组件，负责前端接入。
type Gateway struct {
	cfacade.Component
	addr string
	ws   *cherryConnector.WSConnector
	hub  *Hub
}

// NewGateway 创建网关组件，addr 形如 ":9104"。
func NewGateway(addr string) *Gateway {
	return &Gateway{addr: addr, hub: NewHub()}
}

// Name 返回组件名。
func (g *Gateway) Name() string { return "ddz_gateway" }

// Init 启动 WebSocket 监听。
func (g *Gateway) Init() {
	g.ws = cherryConnector.NewWS(g.addr)
	if g.ws == nil {
		panic("websocket connector 初始化失败: " + g.addr)
	}
	g.ws.OnConnect(g.hub.OnConnect)
}

// OnAfterInit 在所有组件初始化完成后启动监听。
func (g *Gateway) OnAfterInit() {
	g.ws.Start()
	fmt.Fprintf(os.Stdout, "[ddz] websocket listening on %s\n", g.addr)
}

// OnStop 关闭监听。
func (g *Gateway) OnStop() {
	if g.ws != nil {
		g.ws.Stop()
	}
}

// Hub 管理连接与唯一房间。
type Hub struct {
	mu       sync.Mutex
	room     *game.Room
	seats    [3]*session
	seq      int
	robotGen int
}

// NewHub 创建房间中枢。
func NewHub() *Hub {
	return &Hub{room: game.NewRoom("classic", 100)}
}

// OnConnect 处理新连接，逐帧读取 JSON 指令。
func (h *Hub) OnConnect(conn net.Conn) {
	s := &session{seat: -1}
	s.send = func(b []byte) error {
		_, err := conn.Write(b)
		return err
	}
	go h.readLoop(conn, s)
}

// readLoop 读取按行分隔的 JSON 指令，逐条分发。
// WebSocket 传输层可能合并或拆分消息，用换行切分可避免粘包与半包。
func (h *Hub) readLoop(conn net.Conn, s *session) {
	defer h.disconnect(conn, s)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			s.SendError("报文格式错误")
			continue
		}
		h.dispatch(s, req)
	}
}

// disconnect 处理断线：座位交机器人托管，避免牌局卡死。
func (h *Hub) disconnect(conn net.Conn, s *session) {
	_ = conn.Close()
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.seat >= 0 && s.seat < 3 && h.seats[s.seat] == s {
		h.seats[s.seat] = nil
		h.room.Lock()
		p := h.room.Players[s.seat]
		if p != nil {
			p.Online = false
			p.IsRobot = true
		}
		h.room.Unlock()
		h.broadcastLocked()
		h.scheduleRobotLocked()
	}
}

// resetLocked 清空房间与座位，准备新一局。调用方需持 hub 锁。
func (h *Hub) resetLocked() {
	h.robotGen++
	h.room.Reset()
	for i := range h.seats {
		if h.seats[i] != nil {
			h.seats[i].seat = -1
		}
		h.seats[i] = nil
	}
}

// dispatch 按指令名分发。
func (h *Hub) dispatch(s *session, req Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch req.Cmd {
	case CmdLogin:
		h.handleLogin(s, req)
	case CmdJoin:
		h.handleJoin(s)
	case CmdReady:
		h.handleReady(s)
	case CmdBid:
		h.handleBid(s, req)
	case CmdDouble:
		h.handleDouble(s, req)
	case CmdPlay:
		h.handlePlay(s, req)
	case CmdPass:
		h.handlePass(s)
	default:
		s.SendError("未知指令: " + req.Cmd)
	}
}

// handleLogin 建立会话身份。
func (h *Hub) handleLogin(s *session, req Request) {
	var d LoginData
	_ = json.Unmarshal(req.Data, &d)
	h.seq++
	s.id = fmt.Sprintf("p%d", h.seq)
	if d.Nickname == "" {
		d.Nickname = fmt.Sprintf("玩家%d", h.seq)
	}
	s.Send(Response{Cmd: CmdEnter, Data: map[string]any{"id": s.id, "nickname": d.Nickname}})
}

// handleJoin 入座，空位由机器人补齐。
func (h *Hub) handleJoin(s *session) {
	if s.seat >= 0 {
		h.pushState(s)
		return
	}
	h.room.Lock()
	phase := h.room.Phase
	h.room.Unlock()
	// 上一局已结算则清空重开，避免旧手牌与分数串到新一局。
	if phase == game.PhaseSettled {
		h.resetLocked()
	}
	seat := -1
	for i := 0; i < 3; i++ {
		if h.seats[i] == nil {
			seat = i
			break
		}
	}
	if seat < 0 {
		s.SendError("房间已满")
		return
	}
	h.seats[seat] = s
	s.seat = seat

	h.room.Lock()
	p := h.room.Players[seat]
	h.room.Unlock()
	if p == nil {
		if _, err := h.room.Join(s.id, fmt.Sprintf("玩家%d", seat+1), false); err != nil {
			s.SendError(err.Error())
			return
		}
	} else {
		h.room.Lock()
		p.ID = s.id
		p.Online = true
		p.IsRobot = false
		h.room.Unlock()
	}
	h.fillRobotsLocked()
	h.broadcastLocked()
}

// fillRobotsLocked 用机器人填满空座。调用方需持 hub 锁。
func (h *Hub) fillRobotsLocked() {
	for i := 0; i < 3; i++ {
		h.room.Lock()
		occupied := h.room.Players[i] != nil
		h.room.Unlock()
		if occupied {
			continue
		}
		if _, err := h.room.Join(fmt.Sprintf("robot-%d", i), robotName(i), true); err == nil {
			h.room.Lock()
			if rp := h.room.Players[i]; rp != nil {
				rp.Online = true
			}
			h.room.Unlock()
			// 机器人入座即准备，让真人入座后可直接开局。
			_ = h.room.Ready(i)
		}
	}
}

// robotName 生成机器人昵称。
func robotName(seat int) string {
	names := [3]string{"小智", "阿福", "豆豆"}
	return names[seat%3]
}

// handleReady 玩家准备。
func (h *Hub) handleReady(s *session) {
	if s.seat < 0 {
		s.SendError("请先入座")
		return
	}
	if err := h.room.Ready(s.seat); err != nil {
		s.SendError(err.Error())
		return
	}
	h.broadcastLocked()
	h.scheduleRobotLocked()
}

// autoDoubleLocked 让机器人在加倍阶段立即做出选择。调用方需持 hub 锁。
func (h *Hub) autoDoubleLocked() {
	h.room.Lock()
	var seats []int
	if h.room.Phase == game.PhaseDoubling {
		for i, p := range h.room.Players {
			if p != nil && p.IsRobot && !p.DoubleDone {
				seats = append(seats, i)
			}
		}
	}
	h.room.Unlock()
	for _, seat := range seats {
		_ = h.room.Double(seat, false)
	}
}

// handleBid 叫分。
func (h *Hub) handleBid(s *session, req Request) {
	var d BidData
	_ = json.Unmarshal(req.Data, &d)
	if err := h.room.Bid(s.seat, d.Score); err != nil {
		s.SendError(err.Error())
		return
	}
	h.autoDoubleLocked()
	h.broadcastLocked()
	h.scheduleRobotLocked()
}

// handleDouble 加倍。
func (h *Hub) handleDouble(s *session, req Request) {
	var d DoubleData
	_ = json.Unmarshal(req.Data, &d)
	if err := h.room.Double(s.seat, d.Double); err != nil {
		s.SendError(err.Error())
		return
	}
	h.broadcastLocked()
	h.scheduleRobotLocked()
}

// handlePlay 出牌。
func (h *Hub) handlePlay(s *session, req Request) {
	var d PlayData
	_ = json.Unmarshal(req.Data, &d)
	if err := h.room.Play(s.seat, d.Cards); err != nil {
		s.SendError(err.Error())
		return
	}
	h.broadcastLocked()
	h.scheduleRobotLocked()
}

// handlePass 过牌。
func (h *Hub) handlePass(s *session) {
	if err := h.room.Pass(s.seat); err != nil {
		s.SendError(err.Error())
		return
	}
	h.broadcastLocked()
	h.scheduleRobotLocked()
}

// scheduleRobotLocked 若当前轮到机器人，则延时触发其行动。调用方需持 hub 锁。
func (h *Hub) scheduleRobotLocked() {
	h.room.Lock()
	phase := h.room.Phase
	current := h.room.Current
	p := h.room.Players[current]
	h.room.Unlock()
	if p == nil || !p.IsRobot {
		return
	}
	if phase != game.PhaseBidding && phase != game.PhaseDoubling && phase != game.PhasePlaying {
		return
	}
	h.robotGen++
	gen := h.robotGen
	seat := current
	time.AfterFunc(robotDelay, func() { h.robotAct(gen, seat) })
}

// robotAct 机器人在其回合执行动作。
func (h *Hub) robotAct(gen, seat int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if gen != h.robotGen {
		return
	}
	h.room.Lock()
	phase := h.room.Phase
	h.room.Unlock()
	switch phase {
	case game.PhaseBidding:
		score := h.room.AIBid(seat)
		if err := h.room.Bid(seat, score); err != nil {
			_ = h.room.Bid(seat, 0)
		}
	case game.PhaseDoubling:
		_ = h.room.Double(seat, false)
	case game.PhasePlaying:
		cards := h.room.AISuggest(seat)
		if len(cards) == 0 {
			if err := h.room.Pass(seat); err != nil {
				_ = err
			}
		} else if err := h.room.Play(seat, cards); err != nil {
			_ = h.room.Pass(seat)
		}
	default:
		return
	}
	h.autoDoubleLocked()
	h.broadcastLocked()
	h.scheduleRobotLocked()
}

// broadcastLocked 向所有已入座连接推送各自视角的状态。
func (h *Hub) broadcastLocked() {
	for seat, s := range h.seats {
		if s == nil {
			continue
		}
		s.Send(Response{Cmd: CmdState, Data: BuildState(h.room, seat)})
	}
}

// pushState 向单个连接推送状态。
func (h *Hub) pushState(s *session) {
	s.Send(Response{Cmd: CmdState, Data: BuildState(h.room, s.seat)})
}
