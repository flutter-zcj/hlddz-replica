"""斗地主服务端协议联调脚本。

模拟一名真人玩家入座，其余座位由服务端机器人补齐，
自动完成叫分、加倍与出牌，验证牌局能完整走到结算。
"""
import asyncio
import json
import sys

import websockets

URL = "ws://127.0.0.1:9104"


def pick_lead(cards):
    """首家出牌：出最小单张。"""
    if not cards:
        return []
    low = min(cards, key=lambda c: (c["rank"], c["suit"]))
    return [low]


async def main():
    async with websockets.connect(URL, max_size=2**22) as ws:
        await ws.send(json.dumps({"cmd": "login", "data": {"nickname": "测试玩家"}}) + "\n")
        await ws.send(json.dumps({"cmd": "join"}) + "\n")
        await ws.send(json.dumps({"cmd": "ready"}) + "\n")

        last_phase = None
        steps = 0
        while steps < 240:
            steps += 1
            try:
                raw = await asyncio.wait_for(ws.recv(), timeout=25)
            except asyncio.TimeoutError:
                print("超时：牌局未推进")
                return 1
            msg = json.loads(raw)
            if msg["cmd"] == "error":
                print("错误:", msg["data"]["message"])
                continue
            if msg["cmd"] != "state":
                print("消息:", msg["cmd"], msg["data"])
                continue

            st = msg["data"]
            phase = st["phase"]
            if phase != last_phase:
                print(f"[{steps}] 阶段 -> {phase} 倍数={st['multiplier']} 底分={st['base_score']}")
                last_phase = phase

            if phase == "已结算":
                for p in st["players"]:
                    print(f"  座位{p['seat']} {p['nickname']} 地主={p['is_landlord']} 得分={p['score']} 余牌={p['card_count']}")
                print("结算完成，倍数:", st["multiplier"], "炸弹:", st["bomb"])
                return 0

            if not st["your_turn"]:
                continue

            if phase == "叫地主":
                await ws.send(json.dumps({"cmd": "bid", "data": {"score": 3}}) + "\n")
                print("  我叫 3 分")
            elif phase == "加倍":
                await ws.send(json.dumps({"cmd": "double", "data": {"double": False}}) + "\n")
                print("  我选择不加倍")
            elif phase == "出牌":
                if st["last_cards"]:
                    # 桌面有待压牌，测试脚本不搜索组合，直接过牌。
                    await ws.send(json.dumps({"cmd": "pass"}) + "\n")
                else:
                    cards = pick_lead(st["my_cards"])
                    if cards:
                        await ws.send(json.dumps({"cmd": "play", "data": {"cards": cards}}) + "\n")
                    else:
                        await ws.send(json.dumps({"cmd": "pass"}) + "\n")
        print("超过最大步数，未到结算")
        return 1


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
