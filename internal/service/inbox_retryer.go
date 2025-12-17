package service

import (
	"context"
	"log"
	"sync"
	"time"

	"go-im/internal/model"
)

// InboxRetryer 用于在 Inbox 写入失败时进行“最终一致”补偿。
// 注意：这是一个最佳努力实现；生产环境通常需要持久化重试（例如 DB outbox / MQ / Redis stream）。
type InboxRetryer interface {
	Enqueue(msg model.TimelineMessage, userIDs []string)
	Stop()
}

type inboxRetryTask struct {
	msg     model.TimelineMessage
	userIDs []string
	attempt int
}

// AsyncInboxRetryer 使用内存队列 + 后台 worker 进行重试。
// - 不阻塞主流程（队列满会丢弃并打日志）
// - 重试采用指数退避
type AsyncInboxRetryer struct {
	inbox InboxWriter

	maxAttempts int
	baseBackoff time.Duration
	maxBackoff  time.Duration
	timeout     time.Duration

	queue chan inboxRetryTask
	stop  chan struct{}
	wg    sync.WaitGroup
}

type InboxRetryOptions struct {
	QueueSize   int
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	Timeout     time.Duration
}

func NewAsyncInboxRetryer(inbox InboxWriter, opts InboxRetryOptions) *AsyncInboxRetryer {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 1024
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 8
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = 200 * time.Millisecond
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 5 * time.Second
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}

	r := &AsyncInboxRetryer{
		inbox:        inbox,
		maxAttempts:  opts.MaxAttempts,
		baseBackoff:  opts.BaseBackoff,
		maxBackoff:   opts.MaxBackoff,
		timeout:      opts.Timeout,
		queue:        make(chan inboxRetryTask, opts.QueueSize),
		stop:         make(chan struct{}),
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

func (r *AsyncInboxRetryer) Enqueue(msg model.TimelineMessage, userIDs []string) {
	if r == nil || r.inbox == nil {
		return
	}
	task := inboxRetryTask{msg: msg, userIDs: userIDs}
	select {
	case r.queue <- task:
	default:
		log.Printf("inbox retry queue full, drop task conv=%s msg_id=%s", msg.ConversationID, msg.MsgID)
	}
}

func (r *AsyncInboxRetryer) Stop() {
	if r == nil {
		return
	}
	close(r.stop)
	r.wg.Wait()
}

func (r *AsyncInboxRetryer) loop() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stop:
			return
		case task := <-r.queue:
			r.handle(task)
		}
	}
}

func (r *AsyncInboxRetryer) handle(task inboxRetryTask) {
	backoff := r.baseBackoff
	for {
		if task.attempt >= r.maxAttempts {
			log.Printf("inbox retry exceeded max attempts, give up conv=%s msg_id=%s", task.msg.ConversationID, task.msg.MsgID)
			return
		}

		task.attempt++
		ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
		err := r.inbox.Append(ctx, task.msg, task.userIDs)
		cancel()
		if err == nil {
			return
		}

		log.Printf("inbox retry failed attempt=%d conv=%s msg_id=%s err=%v", task.attempt, task.msg.ConversationID, task.msg.MsgID, err)
		if backoff > r.maxBackoff {
			backoff = r.maxBackoff
		}
		select {
		case <-time.After(backoff):
		case <-r.stop:
			return
		}
		backoff *= 2
	}
}

