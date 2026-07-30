package link

import (
	"testing"
	"time"
)

// healthySize/badSize give a ~16x cost ratio at the same RTT (65536/4096),
// comfortably saturating the gradient floor (minCostGradient=0.7) on every
// adjustment — the same trick TestLimiterShrinksOnSustainedRisingDelay uses
// with RTT, just applied to size instead, so RTT (and therefore the gating
// shortRTT) can stay constant and small throughout every test here.
const (
	healthySize = 64 << 10
	badSize     = sizerMinSampleSize // 4 KiB, right at the valid-sample floor
	gateRTT     = 20 * time.Millisecond
	// gateSleep must clear the gate interval (shortRTT*sizerGateMultiplier,
	// ~80ms once shortRTT converges to gateRTT), with margin.
	gateSleep = 120 * time.Millisecond
)

func TestSizerGrowsOnHealthyCost(t *testing.T) {
	s := newSizer()
	start := s.targetSize()

	s.onAck(gateRTT, healthySize) // первый валидный сэмпл только устанавливает baseline
	if s.targetSize() != start {
		t.Fatalf("first sample should only establish baseline, not adjust: got %d want %d", s.targetSize(), start)
	}

	s.onAck(gateRTT, healthySize) // тот же cost — gradient≈1, target должен подрасти
	if s.targetSize() <= start {
		t.Fatalf("target did not grow on healthy cost: start=%d now=%d", start, s.targetSize())
	}
}

func TestSizerRapidAcksDoNotCompound(t *testing.T) {
	// Тот же класс регрессии, что TestLimiterRapidAcksDoNotCompound: пачка
	// сэмплов, пришедших почти одновременно, не должна двигать target больше
	// одного раза за характерный RTT.
	s := newSizer()
	s.onAck(gateRTT, healthySize) // устанавливает baseline
	s.onAck(gateRTT, healthySize) // первая настройка проходит (lastAdjust ещё нулевой)
	before := s.targetSize()

	for i := 0; i < 200; i++ {
		s.onAck(gateRTT, badSize) // резко возросшая стоимость, без пауз между вызовами
	}
	after := s.targetSize()

	if after != before {
		t.Fatalf("burst of near-simultaneous samples adjusted the target more than once: before=%d after=%d", before, after)
	}
	if after < minTarget {
		t.Fatalf("target fell below floor: %d", after)
	}
}

func TestSizerShrinksOnSustainedRisingCost(t *testing.T) {
	s := newSizer()
	s.onAck(gateRTT, healthySize)
	// Дать target вырасти на нескольких сэмплах, разнесённых во времени
	// достаточно, чтобы каждый прошёл гейт "не чаще раза за shortRTT".
	for i := 0; i < 5; i++ {
		time.Sleep(gateSleep)
		s.onAck(gateRTT, healthySize)
	}
	grown := s.targetSize()
	if grown <= minTarget {
		t.Fatalf("precondition failed: target did not grow (=%d)", grown)
	}

	for i := 0; i < 3; i++ {
		time.Sleep(gateSleep)
		s.onAck(gateRTT, badSize) // устойчиво возросшая стоимость (тот же RTT, меньший размер)
	}
	if s.targetSize() >= grown {
		t.Fatalf("target did not shrink on sustained rising cost: grown=%d now=%d", grown, s.targetSize())
	}
}

func TestSizerIgnoresSamplesBelowMinSize(t *testing.T) {
	s := newSizer()
	start := s.targetSize()

	for i := 0; i < 10; i++ {
		time.Sleep(gateSleep)
		s.onAck(gateRTT, 100) // много меньше sizerMinSampleSize
	}

	if s.targetSize() != start {
		t.Fatalf("samples below sizerMinSampleSize moved the target: start=%d now=%d", start, s.targetSize())
	}
}

func TestSizerClampedToFloor(t *testing.T) {
	s := newSizer()
	s.onAck(gateRTT, healthySize)
	for i := 0; i < 25; i++ {
		time.Sleep(gateSleep)
		s.onAck(gateRTT, badSize)
	}
	if got := s.targetSize(); got < minTarget {
		t.Fatalf("target fell below floor: %d < %d", got, minTarget)
	}
}

func TestSizerClampedToCeiling(t *testing.T) {
	s := newSizer()
	s.onAck(gateRTT, healthySize)
	for i := 0; i < 25; i++ {
		time.Sleep(gateSleep)
		s.onAck(gateRTT, healthySize)
	}
	if got := s.targetSize(); got > maxTarget {
		t.Fatalf("target exceeded ceiling: %d > %d", got, maxTarget)
	}
}
