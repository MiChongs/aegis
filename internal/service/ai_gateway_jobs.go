package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
	"sync"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 异步任务（docs/ai-gateway.md「异步任务」）。
//
// 生图、改图、转写常要一两分钟，而网关前面的每一层反向代理都有读超时（常见 60–100 秒）：
// 同步调用挂过这个时间，客户端拿到的是代理的 504 页面，网关这边看到的是客户端断开 ——
// 调用不扣费，但用户什么也没拿到。异步任务把一次调用拆成两段短请求：
//
//  1. 提交：鉴权、组装请求体、暂扣都在请求里同步做（余额不足等错误照常立刻返回），
//     然后登记任务、立刻回 202 与任务编号（就是调用编号）；
//  2. 后台调上游、结算，结果（交给客户端的响应体原样）写进 ai_gateway_jobs；
//     客户端 GET /jobs/{id}（可带 wait 长轮询）取回，DELETE 取消。
//
// 计费口径与同步调用完全一样：成功才扣，失败与取消整笔放回。

const (
	// aiGatewayJobTimeout 一个任务在后台最多跑多久（上游调用自己还有 10 分钟上限）。
	aiGatewayJobTimeout = 12 * time.Minute
	// aiGatewayJobMaxWait 长轮询最多挂多久：要短于前面反向代理的读超时。
	aiGatewayJobMaxWait = 25 * time.Second
	// aiGatewayJobPollAfter 建议客户端的轮询间隔。
	aiGatewayJobPollAfter = 2 * time.Second
	// aiGatewayJobReplayWait / aiGatewayJobReplayPoll 同键重复提交时，等原任务行写进来最多多久、多久查一次。
	aiGatewayJobReplayWait = 3 * time.Second
	aiGatewayJobReplayPoll = 150 * time.Millisecond
	// aiGatewayJobDrainTimeout 进程退出时等进行中的任务收尾多久，超过就取消（取消即放回、不扣费）。
	aiGatewayJobDrainTimeout = 20 * time.Second
)

// AIGatewayJobInput 一次异步提交。
type AIGatewayJobInput struct {
	Passthrough AIGatewayPassthroughInput
	// Render 把成功的结果变成交给客户端的响应体，与同步调用写出的完全相同。
	Render func(*AIGatewayPassthroughResult) (contentType string, body []byte)
	// Fail 把错误变成错误信封，与同步调用的错误响应一致。
	Fail func(error) aigateway.JobError
}

// aiGatewayJobRegistry 本实例上正在跑的任务：取消要找到它的 cancel，退出时要等它们收尾。
type aiGatewayJobRegistry struct {
	mu       sync.Mutex
	running  map[string]context.CancelFunc
	canceled map[string]bool
	// finished 任务写完结论时关闭：本机上的长轮询据此立刻返回，不必等下一次查库。
	finished map[string]chan struct{}
	wg       sync.WaitGroup
}

func newAIGatewayJobRegistry() *aiGatewayJobRegistry {
	return &aiGatewayJobRegistry{running: map[string]context.CancelFunc{}, canceled: map[string]bool{},
		finished: map[string]chan struct{}{}}
}

func (r *aiGatewayJobRegistry) add(requestNo string, cancel context.CancelFunc) {
	r.mu.Lock()
	r.running[requestNo] = cancel
	r.finished[requestNo] = make(chan struct{})
	r.mu.Unlock()
	r.wg.Add(1)
}

// remove 任务结束（结论已写库）。
func (r *aiGatewayJobRegistry) remove(requestNo string) {
	r.mu.Lock()
	if done, ok := r.finished[requestNo]; ok {
		close(done)
	}
	delete(r.running, requestNo)
	delete(r.canceled, requestNo)
	delete(r.finished, requestNo)
	r.mu.Unlock()
	r.wg.Done()
}

// finishedSignal 本机上这个任务结束时关闭的通道；不在本机返回 nil（nil 通道永远不就绪）。
func (r *aiGatewayJobRegistry) finishedSignal(requestNo string) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished[requestNo]
}

func (r *aiGatewayJobRegistry) wasCanceled(requestNo string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.canceled[requestNo]
}

// cancel 取消本机上的一个任务；不在本机返回 false。
func (r *aiGatewayJobRegistry) cancel(requestNo string) bool {
	r.mu.Lock()
	cancel, ok := r.running[requestNo]
	if ok {
		r.canceled[requestNo] = true
	}
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// drain 等进行中的任务收尾，超时后全部取消再等一小会儿。
func (r *aiGatewayJobRegistry) drain(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(timeout):
	}
	r.mu.Lock()
	for _, cancel := range r.running {
		cancel()
	}
	r.mu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// SubmitJob 异步提交一次原样转发：暂扣成功后立刻返回进行中的任务，上游调用在后台跑。
//
// 带 Idempotency-Key 重复提交（客户端没收到上一次的响应、断网后续交、进程被杀后恢复）时不再建新任务、
// 不再暂扣，而是原样返回上一次提交建立的那个任务，replayed 为真。这样客户端可以放心地用同一个键重发，
// 结论只有一个：要么拿到原任务接着轮询，要么上一次确实没成（失败或过期的不占键），这次正常提交。
func (s *AIGatewayService) SubmitJob(ctx context.Context, principal *AIGatewayPrincipal, in AIGatewayJobInput) (view *aigateway.JobView, replayed bool, err error) {
	prepared, err := s.preparePassthrough(ctx, principal, in.Passthrough)
	if err != nil {
		if existing := s.replayJob(ctx, principal, err); existing != nil {
			return existing, true, nil
		}
		return nil, false, err
	}
	now := s.now()
	if err := s.pg.CreateAIGatewayJob(context.WithoutCancel(ctx), prepared.requestNo, in.Passthrough.Endpoint, now); err != nil {
		// 还没调上游：放回暂扣，本次不扣费。
		cause := fmt.Errorf("登记异步任务失败：%w", err)
		prepared.call.finish(ctx, s.release(ctx, prepared.requestNo, cause, nil, now), nil, aidomain.FinishError)
		return nil, false, cause
	}
	jobCtx, cancel := context.WithTimeout(context.Background(), aiGatewayJobTimeout)
	s.jobs.add(prepared.requestNo, cancel)
	go s.runJob(jobCtx, cancel, prepared, in)
	return aiGatewayJobView(&aigateway.Job{RequestNo: prepared.requestNo, AppID: principal.AppID, UserID: principal.UserID,
		Endpoint: in.Passthrough.Endpoint, Status: aigateway.JobInProgress, CreatedAt: now}, now), false, nil
}

// replayJob 提交撞上同键的已有调用时，找出那次调用的任务。
//
// 暂扣与登记任务是两步：同键的第二次请求可能恰好落在两步之间，这时任务行还没写进去，稍等再查。
// 那次调用不是异步任务（同步调用占了这个键）或者等不到任务行时返回 nil，调用方照常回 40213（带 request_no）。
func (s *AIGatewayService) replayJob(ctx context.Context, principal *AIGatewayPrincipal, err error) *aigateway.JobView {
	conflict, ok := AIGatewayIdempotencyConflictOf(err)
	if !ok {
		return nil
	}
	deadline := time.Now().Add(aiGatewayJobReplayWait)
	for {
		job, loadErr := s.pg.GetAIGatewayJob(ctx, principal.AppID, principal.UserID, conflict.RequestNo)
		if loadErr == nil {
			return aiGatewayJobView(job, s.now())
		}
		if !errors.Is(loadErr, pgrepo.ErrAIGatewayJobNotFound) || !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(aiGatewayJobReplayPoll):
		}
	}
}

func (s *AIGatewayService) runJob(ctx context.Context, cancel context.CancelFunc, prepared *preparedPassthrough, in AIGatewayJobInput) {
	requestNo := prepared.requestNo
	var (
		result *AIGatewayPassthroughResult
		err    error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("AI 异步任务 panic", zap.String("requestNo", requestNo), zap.Any("panic", r))
				err = fmt.Errorf("任务执行异常：%v", r)
			}
		}()
		result, err = s.executePassthrough(ctx, prepared)
	}()
	canceled := s.jobs.wasCanceled(requestNo)
	cancel()

	finishCtx, finishCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer finishCancel()
	defer s.jobs.remove(requestNo)
	now := s.now()
	var writeErr error
	switch {
	case err == nil:
		contentType, body := in.Render(result)
		_, writeErr = s.pg.FinishAIGatewayJob(finishCtx, requestNo, aigateway.JobCompleted, 200, contentType, body, nil, now)
	case canceled:
		_, writeErr = s.pg.FinishAIGatewayJob(finishCtx, requestNo, aigateway.JobCanceled, 499, "", nil,
			&aigateway.JobError{Message: "任务已取消，本次未扣费", Type: "invalid_request_error", Code: "job_canceled", Status: 499}, now)
	default:
		jobErr := in.Fail(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			jobErr = aigateway.JobError{Message: "任务超时或服务重启，本次未扣费，请重新提交", Type: "api_error",
				Code: "job_interrupted", Status: 504}
		}
		_, writeErr = s.pg.FinishAIGatewayJob(finishCtx, requestNo, aigateway.JobFailed, jobErr.Status, "", nil, &jobErr, now)
	}
	if writeErr != nil {
		// 结论写不进去：任务会被后台按超时判为中断。已经结算的调用照常计费，结果丢失时管理员可在调用记录里退款。
		s.log.Error("AI 异步任务结论写入失败", zap.String("requestNo", requestNo), zap.Error(writeErr))
	}
}

// GetJob 取任务。wait > 0 时长轮询：任务还在进行中就最多等这么久（上限 25 秒），有结论立刻返回。
func (s *AIGatewayService) GetJob(ctx context.Context, principal *AIGatewayPrincipal, requestNo string, wait time.Duration) (*aigateway.JobView, error) {
	job, err := s.loadJob(ctx, principal, requestNo, wait)
	if err != nil {
		return nil, err
	}
	return aiGatewayJobView(job, s.now()), nil
}

// JobContent 已完成任务的结果原样（二进制结果走这里）。
func (s *AIGatewayService) JobContent(ctx context.Context, principal *AIGatewayPrincipal, requestNo string) (*aigateway.Job, error) {
	job, err := s.loadJob(ctx, principal, requestNo, 0)
	if err != nil {
		return nil, err
	}
	if job.Status != aigateway.JobCompleted {
		return nil, aiGatewayError(40227, 409, "任务还没有完成")
	}
	return job, nil
}

// CancelJob 取消进行中的任务（广播给跑它的实例），稍等片刻返回最新状态。已结束的任务原样返回。
func (s *AIGatewayService) CancelJob(ctx context.Context, principal *AIGatewayPrincipal, requestNo string) (*aigateway.JobView, error) {
	job, err := s.loadJob(ctx, principal, requestNo, 0)
	if err != nil {
		return nil, err
	}
	if job.Status == aigateway.JobInProgress {
		s.live.publishCancel(job.RequestNo)
		if updated, err := s.loadJob(ctx, principal, requestNo, 5*time.Second); err == nil {
			job = updated
		}
	}
	return aiGatewayJobView(job, s.now()), nil
}

func (s *AIGatewayService) loadJob(ctx context.Context, principal *AIGatewayPrincipal, requestNo string, wait time.Duration) (*aigateway.Job, error) {
	requestNo = strings.TrimSpace(requestNo)
	wait = min(max(wait, 0), aiGatewayJobMaxWait)
	deadline := time.Now().Add(wait)
	for {
		job, err := s.pg.GetAIGatewayJob(ctx, principal.AppID, principal.UserID, requestNo)
		if err != nil {
			return nil, mapAIGatewayError(err)
		}
		if job.Status != aigateway.JobInProgress || !time.Now().Before(deadline) {
			return job, nil
		}
		select {
		case <-ctx.Done():
			return job, nil
		case <-s.jobs.finishedSignal(requestNo):
		case <-time.After(min(time.Second, time.Until(deadline))):
		}
	}
}

// aiGatewayJobView 存储形状 → 对外形状。JSON 结果内嵌，其余给取结果的地址。
func aiGatewayJobView(job *aigateway.Job, now time.Time) *aigateway.JobView {
	view := &aigateway.JobView{
		ID: job.RequestNo, Object: aigateway.JobObject, Endpoint: job.Endpoint, Status: job.Status,
		CreatedAt: job.CreatedAt.Unix(), Error: job.Error,
	}
	end := now
	if job.FinishedAt != nil {
		end = *job.FinishedAt
		completed := job.FinishedAt.Unix()
		view.CompletedAt = &completed
	}
	view.ElapsedMs = max(end.Sub(job.CreatedAt).Milliseconds(), 0)
	switch job.Status {
	case aigateway.JobInProgress:
		view.PollAfterMs = int(aiGatewayJobPollAfter.Milliseconds())
	case aigateway.JobCompleted:
		view.ContentType = job.ContentType
		mediaType, _, _ := mime.ParseMediaType(job.ContentType)
		if (mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")) && json.Valid(job.Result) {
			view.Result = json.RawMessage(job.Result)
		} else {
			view.ResultURL = RelayBasePath + "/jobs/" + job.RequestNo + "/content"
		}
	}
	return view
}
