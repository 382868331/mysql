// 本题（bugfix_mysql_003）隐藏回归测试：压缩连接发送 PING 前压缩层与普通包层
// sequence 同步错误。
//
// 目标现象：压缩连接发送 PING 前没有同步压缩层与普通包层的 sequence，服务端拒绝
// 序号不一致的数据包。
package mysql

import (
	"bytes"
	"compress/zlib"
	"testing"
)

// taskMyq03PlainPacket 构造一个普通包：4 字节头（len24 + seq）+ payload。
func taskMyq03PlainPacket(payload []byte, seq uint8) []byte {
	pkt := make([]byte, 4+len(payload))
	pkt[0] = byte(len(payload))
	pkt[1] = byte(len(payload) >> 8)
	pkt[2] = byte(len(payload) >> 16)
	pkt[3] = seq
	copy(pkt[4:], payload)
	return pkt
}

// taskMyq03CompressedPackets 把一串普通包合并进一个压缩包（压缩 seq=compSeq），
// 模拟服务器 net_flush 缓冲合并行为。
func taskMyq03CompressedPackets(packets [][]byte, compSeq uint8) []byte {
	var plain bytes.Buffer
	for _, p := range packets {
		plain.Write(p)
	}
	var comp bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&comp, 2)
	zw.Write(plain.Bytes())
	zw.Close()
	cl := comp.Len()
	header := make([]byte, 7)
	header[0] = byte(cl)
	header[1] = byte(cl >> 8)
	header[2] = byte(cl >> 16)
	header[3] = compSeq
	header[4] = byte(plain.Len())
	header[5] = byte(plain.Len() >> 8)
	header[6] = byte(plain.Len() >> 16)
	return append(header, comp.Bytes()...)
}

// TestTaskMYQ03CompressedPingSync 验证压缩连接上发送 PING 前压缩层与普通包层
// sequence 已同步：writeCommandPacket(comPing) 写包后必须完成 syncSequence
// （同步 sequence 并清空压缩写缓冲），使 PING 的服务器响应能被完整、正确地读取。
//
// 缺陷复现：writeCommandPacket 缺少 syncSequence（writeCommandPacketStr 有），
// PING 包写入后 compIO.buff 残留本次发送的数据；随后 readResultOK 通过
// compIO.readNext 先消费残留缓冲，把自身发送内容当作服务器响应解析，真实服务器
// 响应滞留未消费，sequence 与服务器错位（服务端拒绝序号不一致的数据包）。
func TestTaskMYQ03CompressedPingSync(t *testing.T) {
	conn, mc := newRWMockConn(0)
	mc.compress = true
	mc.compIO = newCompIO(mc)
	mc.capabilities = clientProtocol41 | clientCompress

	// 模拟压缩连接上多包响应后的失步前置状态（sequence 与 compressSequence 不同步）。
	mc.sequence = 2
	mc.compressSequence = 4

	// 服务器对 PING 的 OK 响应：压缩包 seq=1，内部普通包 seq=1。
	okPayload := []byte{0x00, 0x00, 0x00, 0x02, 0x00}
	conn.data = taskMyq03CompressedPackets([][]byte{taskMyq03PlainPacket(okPayload, 1)}, 1)

	// 压缩连接上发送 PING 命令。
	if err := mc.writeCommandPacket(comPing); err != nil {
		t.Fatalf("writeCommandPacket(comPing): %v", err)
	}

	// 读取 PING 的 OK 响应。
	if err := mc.clearResult().readResultOK(); err != nil {
		t.Fatalf("readResultOK after PING: %v", err)
	}

	// 服务器响应必须被完整消费。若 PING 前未同步（写缓冲残留自身数据被当作响应
	// 读取），服务器响应会滞留在这里 → 失败。
	if len(conn.data) != 0 {
		t.Fatalf("PING server response not consumed (leftover=%x): compressed connection sequence not synchronized before PING", conn.data)
	}

	// 读响应后压缩层与普通包层 sequence 应同步。
	if mc.sequence != mc.compressSequence {
		t.Fatalf("sequence desync after PING round-trip: sequence=%d compressSequence=%d", mc.sequence, mc.compressSequence)
	}
}
