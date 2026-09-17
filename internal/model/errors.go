package model

import "errors"

// 农机调度的领域错误，repository/service 共用，handler 据此映射 HTTP 状态码
var (
	ErrMachineBroken         = errors.New("machine is broken")
	ErrMachineNotBroken      = errors.New("machine is not broken")
	ErrInvalidTransition     = errors.New("invalid reservation status transition")
	ErrEarlierStepUnfinished = errors.New("earlier steps on this plot are not finished")
)
