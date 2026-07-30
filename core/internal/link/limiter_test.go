package link

import (
	"testing"
	"time"
)

func TestLimiterGrowsOnHealthyRTT(t *testing.T) {
	l := newLimiter()
	start := l.window()

	l.onAck(50 * time.Millisecond) // первый сэмпл только устанавливает RTT, лимит не трогает
	if l.window() != start {
		t.Fatalf("first sample should only establish RTT, not adjust: got %d want %d", l.window(), start)
	}

	l.onAck(50 * time.Millisecond) // тот же RTT — gradient≈1, лимит должен подрасти
	if l.window() <= start {
		t.Fatalf("limit did not grow on healthy RTT: start=%d now=%d", start, l.window())
	}
}

func TestLimiterRapidAcksDoNotCompound(t *testing.T) {
	// Регрессия на обвал cwnd=87→1 старого AIMD: пачка ACK, пришедших почти
	// одновременно (без ощутимого реального времени между вызовами), не
	// должна двигать лимит больше одного раза за RTT.
	l := newLimiter()
	l.onAck(50 * time.Millisecond) // устанавливает базовый RTT
	l.onAck(50 * time.Millisecond) // первая настройка проходит (lastAdjust ещё нулевой)
	before := l.window()

	for i := 0; i < 200; i++ {
		l.onAck(500 * time.Millisecond) // резкий рост задержки, без пауз между вызовами
	}
	after := l.window()

	if after != before {
		t.Fatalf("burst of near-simultaneous ACKs adjusted the limit more than once: before=%d after=%d", before, after)
	}
	if after < minLimit {
		t.Fatalf("limit fell below floor: %d", after)
	}
}

func TestLimiterShrinksOnSustainedRisingDelay(t *testing.T) {
	l := newLimiter()
	l.onAck(20 * time.Millisecond)
	// Дать лимиту вырасти: несколько сэмплов, разнесённых во времени достаточно,
	// чтобы каждый прошёл гейт "не чаще раза за RTT".
	for i := 0; i < 5; i++ {
		time.Sleep(25 * time.Millisecond)
		l.onAck(20 * time.Millisecond)
	}
	grown := l.window()
	if grown <= minLimit {
		t.Fatalf("precondition failed: limit did not grow (=%d)", grown)
	}

	// Пауза с запасом покрывает shortRTT даже после того, как он сам подрастёт
	// вслед за возросшим RTT (иначе гейт "не чаще раза за RTT" не пропустит
	// пересчёт — сам shortRTT успевает вырасти быстрее фиксированной паузы).
	for i := 0; i < 3; i++ {
		time.Sleep(250 * time.Millisecond)
		l.onAck(200 * time.Millisecond) // устойчиво возросшая задержка
	}
	if l.window() >= grown {
		t.Fatalf("limit did not shrink on sustained rising delay: grown=%d now=%d", grown, l.window())
	}
}

func TestPacingIntervalPositiveAfterRTT(t *testing.T) {
	l := newLimiter()
	if l.pacingInterval() != 0 {
		t.Fatal("pacing interval should be 0 before any RTT sample")
	}
	l.onAck(120 * time.Millisecond)
	if l.pacingInterval() <= 0 {
		t.Fatal("pacing interval should be positive once an RTT is known")
	}
}
