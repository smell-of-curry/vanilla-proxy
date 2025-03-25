package utils

import "time"

type DelayedTask struct {
	Delay  int64
	action func()
}

type DelayedRepeatingTask struct {
	Delay   int64
	Seconds int64
	action  func()
	stop    chan struct{}
}

type RepeatingTask struct {
	Seconds int64
	action  func()
	stop    chan struct{}
}

func NewDelayedTask(delay int64, action func()) *DelayedTask {
	dt := &DelayedTask{
		Delay:  delay,
		action: action,
	}

	go func() {
		time.Sleep(time.Duration(dt.Delay) * time.Second)
		dt.onRun()
	}()

	return dt
}

func (dt *DelayedTask) onRun() {
	dt.action()
}

func NewDelayedRepeatingTask(delay, seconds int64, action func()) *DelayedRepeatingTask {
	drt := &DelayedRepeatingTask{
		Delay:   delay,
		Seconds: seconds,
		action:  action,
		stop:    make(chan struct{}),
	}

	go func() {
		time.Sleep(time.Duration(drt.Delay) * time.Second)
		drt.onRun()

		for {
			select {
			case <-drt.stop:
				return
			default:
				time.Sleep(time.Duration(drt.Seconds) * time.Second)
				drt.onRun()
			}
		}
	}()

	return drt
}

func (drt *DelayedRepeatingTask) onRun() {
	drt.action()
}

func (drt *DelayedRepeatingTask) Stop() {
	close(drt.stop)
}

func NewRepeatingTask(seconds int64, action func()) *RepeatingTask {
	drt := &RepeatingTask{
		Seconds: seconds,
		action:  action,
		stop:    make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-drt.stop:
				return
			default:
				drt.onRun()
				time.Sleep(time.Duration(drt.Seconds) * time.Second)
			}
		}
	}()

	return drt
}

func (drt *RepeatingTask) onRun() {
	drt.action()
}

func (drt *RepeatingTask) Stop() {
	close(drt.stop)
}
