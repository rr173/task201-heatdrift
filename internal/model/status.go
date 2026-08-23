package model

import (
	"errors"
	"fmt"
)

// 领域错误。HTTP 层据此映射状态码。
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrInvalid       = errors.New("invalid input")
	ErrBadState      = errors.New("invalid state transition")
	ErrOutOfOrder    = errors.New("sequence out of order")
	ErrOutOfBounds   = errors.New("coordinates out of bounds")
	ErrUnknownDevice = errors.New("unknown device")
	ErrFrozen        = errors.New("version frozen, direct overwrite rejected")
)

// StateError 携带期望与实际状态的流转错误。
type StateError struct {
	Entity string
	From   string
	To     string
}

func (e *StateError) Error() string {
	return fmt.Sprintf("invalid %s transition: %s -> %s", e.Entity, e.From, e.To)
}

// Is 支持 errors.Is 匹配 ErrBadState。
func (e *StateError) Is(target error) bool {
	return target == ErrBadState
}

// ---------- 状态机流转校验 ----------

var (
	missionTransitions = map[string]map[string]bool{
		"running":   {"gapped": true, "cleaning": true, "completed": true},
		"gapped":    {"cleaning": true, "completed": true, "running": true},
		"cleaning":  {"running": true, "completed": true},
	"completed": {"completed": true},
	}
	obsTransitions = map[string]map[string]bool{
		"pending": {"matched": true, "jump": true, "discarded": true},
		"matched": {"discarded": true},
		"jump":    {"matched": true, "discarded": true},
		"discarded": {},
	}
	segmentTransitions = map[string]map[string]bool{
		"draft":     {"corrected": true, "review": true},
		"corrected": {"review": true, "confirmed": true},
		"review":    {"confirmed": true, "corrected": true},
		"confirmed": {},
	}
	versionTransitions = map[string]map[string]bool{
		"computing":  {"published": true, "superseded": true},
		"published":  {"superseded": true},
		"superseded": {},
	}
)

// TransitionMission 校验任务状态流转。
func TransitionMission(from, to string) error {
	return transition("mission", missionTransitions, from, to)
}

// TransitionObservation 校验观测点状态流转。
func TransitionObservation(from, to string) error {
	return transition("observation", obsTransitions, from, to)
}

// TransitionSegment 校验轨迹段状态流转。
func TransitionSegment(from, to string) error {
	return transition("segment", segmentTransitions, from, to)
}

// TransitionVersion 校验版本状态流转。
func TransitionVersion(from, to string) error {
	return transition("version", versionTransitions, from, to)
}

func transition(entity string, table map[string]map[string]bool, from, to string) error {
	next, ok := table[from]
	if !ok {
		return &StateError{Entity: entity, From: from, To: to}
	}
	if !next[to] {
		return &StateError{Entity: entity, From: from, To: to}
	}
	return nil
}
