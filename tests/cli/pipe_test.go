package cli_test

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"

	gogentv1 "github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

func TestExecuteToolPipeline(t *testing.T) {
	basePort := 9598
	httpPort := basePort + 100 // HTTP on 9698
	grpcAddr := fmt.Sprintf("127.0.0.1:%d", basePort+1)

	cmd := exec.Command(gogentExe, "daemon", "--port", strconv.Itoa(httpPort), "--base-port", strconv.Itoa(basePort), "--foreground")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc dial: %v", err)
	}
	defer conn.Close()

	// Wait for gRPC to be ready
	for time.Now().Before(deadline) {
		client := gogentv1.NewToolServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := client.GetToolStatus(ctx, &gogentv1.GetToolStatusRequest{Name: "calculator"})
		cancel()
		if err == nil { break }
		time.Sleep(300 * time.Millisecond)
	}

	client := gogentv1.NewToolServiceClient(conn)

	t.Run("normal-execution", func(t *testing.T) {
		testNormalExecution(t, client)
	})

	t.Run("hook-blocked", func(t *testing.T) {
		testHookBlocked(t, client)
	})

	t.Run("event-sequencing", func(t *testing.T) {
		testEventSequencing(t, client)
	})
}

func testNormalExecution(t *testing.T, client gogentv1.ToolServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.ExecuteTool(ctx)
	if err != nil { t.Fatalf("open stream: %v", err) }
	defer stream.CloseSend()

	params, _ := structpb.NewStruct(map[string]any{"expr": "1+1"})
	stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Request{
		Request: &gogentv1.ToolExecuteRequest{ToolName: "calculator", Params: params},
	}})

	// 1. Auth
	evt := mustRead(t, stream)
	auth := evt.GetAuth()
	if auth == nil { t.Fatal("[1] expected Auth") }
	if !auth.Passed { t.Fatal("[1] auth not passed") }
	if evt.Sequence != 1 { t.Errorf("[1] seq=%d, want 1", evt.Sequence) }
	t.Logf("Auth: passed=%v level=%d", auth.Passed, auth.EffectiveLevel)

	// 2. Pre Hook
	evt = mustRead(t, stream)
	hook := evt.GetHook()
	if hook == nil {
		t.Fatalf("[2] expected Hook, got event type: auth=%v hook=%v result=%v sandbox=%v seq=%d",
			evt.GetAuth() != nil, evt.GetHook() != nil, evt.GetResult() != nil, evt.GetSandbox() != nil, evt.Sequence)
	}
	if hook.Stage != "pre_execute" { t.Errorf("[2] stage=%s, want pre_execute", hook.Stage) }
	t.Logf("PreHook: id=%s stage=%s", hook.HookId, hook.Stage)

	// Reply approved
	stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Verdict{
		Verdict: &gogentv1.HookVerdict{HookId: hook.HookId, Approved: true},
	}})

	// 3. Post Hook (comes before Result so it can modify output)
	evt = mustRead(t, stream)
	postHook := evt.GetHook()
	if postHook == nil { t.Fatal("[3] expected Post Hook") }
	if postHook.Stage != "post_execute" { t.Errorf("[3] stage=%s, want post_execute", postHook.Stage) }
	t.Logf("PostHook: id=%s stage=%s", postHook.HookId, postHook.Stage)

	// Reply (no output modification)
	stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Verdict{
		Verdict: &gogentv1.HookVerdict{HookId: postHook.HookId},
	}})

	// 4. Result
	evt = mustRead(t, stream)
	result := evt.GetResult()
	if result == nil { t.Fatal("[4] expected Result") }
	if result.IsError { t.Errorf("[4] unexpected error: %s", result.ErrorMsg) }
	t.Logf("Result: is_error=%v msg=%s", result.IsError, result.ErrorMsg)

	// EOF
	_, err = stream.Recv()
	if err == nil { t.Error("expected EOF after result") }
	t.Logf("Stream closed: %v", err)
}

func testHookBlocked(t *testing.T, client gogentv1.ToolServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.ExecuteTool(ctx)
	if err != nil { t.Fatalf("open stream: %v", err) }
	defer stream.CloseSend()

	params, _ := structpb.NewStruct(map[string]any{"expr": "1+1"})
	stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Request{
		Request: &gogentv1.ToolExecuteRequest{ToolName: "calculator", Params: params},
	}})

	// 1. Auth
	evt := mustRead(t, stream)
	if evt.GetAuth() == nil { t.Fatal("[1] expected Auth") }

	// 2. Pre Hook — reject
	evt = mustRead(t, stream)
	hook := evt.GetHook()
	if hook == nil {
		t.Fatalf("[2] expected Hook, got event type: auth=%v hook=%v result=%v sandbox=%v seq=%d",
			evt.GetAuth() != nil, evt.GetHook() != nil, evt.GetResult() != nil, evt.GetSandbox() != nil, evt.Sequence)
	}
	stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Verdict{
		Verdict: &gogentv1.HookVerdict{
			HookId:   hook.HookId,
			Approved: false,
			Reason:   "forbidden by test",
		},
	}})

	// 3. Error Result (no execution)
	evt = mustRead(t, stream)
	result := evt.GetResult()
	if result == nil { t.Fatal("[3] expected Result") }
	if !result.IsError { t.Error("[3] expected is_error=true") }
	if !strings.Contains(result.ErrorMsg, "blocked by hook") {
		t.Errorf("[3] error msg=%q, want 'blocked by hook'", result.ErrorMsg)
	}
	t.Logf("Blocked: %s", result.ErrorMsg)

	// Stream should close — no post hook
	_, err = stream.Recv()
	if err == nil { t.Error("expected EOF after blocked result") }
	t.Logf("Stream closed: %v", err)
}

func testEventSequencing(t *testing.T, client gogentv1.ToolServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.ExecuteTool(ctx)
	if err != nil { t.Fatalf("open stream: %v", err) }
	defer stream.CloseSend()

	stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Request{
		Request: &gogentv1.ToolExecuteRequest{ToolName: "calculator"},
	}})

	var seqs []int64
	for i := 0; i < 4; i++ {
		evt, err := stream.Recv()
		if err != nil { break }
		seqs = append(seqs, evt.Sequence)

		if hook := evt.GetHook(); hook != nil {
			stream.Send(&gogentv1.ToolControl{Msg: &gogentv1.ToolControl_Verdict{
				Verdict: &gogentv1.HookVerdict{HookId: hook.HookId, Approved: true},
			}})
		}
	}

	t.Logf("Sequences: %v", seqs)
	if len(seqs) < 4 { t.Fatalf("expected >=4 events (Auth+Hook:pre+Hook:post+Result), got %d: %v", len(seqs), seqs) }
	for i := 0; i < len(seqs)-1; i++ {
		if seqs[i+1] <= seqs[i] {
			t.Errorf("seq[%d]=%d not less than seq[%d]=%d", i, seqs[i], i+1, seqs[i+1])
		}
	}
	if seqs[0] != 1 { t.Errorf("first seq=%d, want 1", seqs[0]) }
}

// --- helpers ---

func mustRead(t *testing.T, stream gogentv1.ToolService_ExecuteToolClient) *gogentv1.ToolExecutionEvent {
	t.Helper()
	ch := make(chan *gogentv1.ToolExecutionEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		evt, err := stream.Recv()
		if err != nil { errCh <- err; return }
		ch <- evt
	}()
	select {
	case evt := <-ch:
		return evt
	case err := <-errCh:
		t.Fatalf("read event: %v", err)
		return nil
	case <-time.After(5 * time.Second):
		t.Fatal("timeout reading event")
		return nil
	}
}
