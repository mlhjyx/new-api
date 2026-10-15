package controller

import (
	"time"

	"github.com/bytedance/gopkg/util/gopool"
)

// waitForTestAsyncWorkers waits, bounded and best effort, until gopool is back
// to the worker count observed before a test started its own async work
// (quota updates read globals such as common.RedisEnabled). Other tests in
// this package can leave long-lived workers running, so waiting for zero
// would never finish; restoring globals only after the test's own workers
// exit keeps the race detector quiet.
func waitForTestAsyncWorkers(baseline int32) {
	deadline := time.Now().Add(3 * time.Second)
	for gopool.WorkerCount() > baseline && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}
