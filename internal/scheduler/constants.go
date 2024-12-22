package scheduler

import "time"

const (
	taskStreamName     = "TASKS"
	taskSubmitSubject  = "task.submit"
	taskStatusSubject  = "task.status"
	taskResultSubject  = "task.result"

	scheduleStreamName    = "SCHEDULES"
	scheduleAddSubject    = "schedule.add"
	scheduleRemoveSubject = "schedule.remove"

	streamMaxAge  = 24 * time.Hour
	streamMaxMsgs = -1
)
