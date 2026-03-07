//go:build linux

// Package probe polls VM health probes via the VSOCK guest agent.
package probe

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
	pb "github.com/syscode-labs/imp/internal/proto/guest"
)

// ConditionPatcher is called when probe results produce updated conditions.
type ConditionPatcher func(conditions []metav1.Condition)

// Runner polls probes on a schedule and calls patcher with updated conditions.
type Runner struct {
	client  pb.GuestAgentClient
	spec    *impv1alpha1.ProbeSpec
	patcher ConditionPatcher
}

// NewRunner creates a Runner that polls probeSpec via client and calls patcher on changes.
func NewRunner(client pb.GuestAgentClient, spec *impv1alpha1.ProbeSpec, patcher ConditionPatcher) *Runner {
	return &Runner{client: client, spec: spec, patcher: patcher}
}

// Run starts polling probes until ctx is cancelled. Call in a goroutine.
func (r *Runner) Run(ctx context.Context) {
	if r.spec == nil {
		return
	}
	if r.spec.StartupProbe != nil {
		go r.pollProbe(ctx, "Started", r.spec.StartupProbe)
	}
	if r.spec.ReadinessProbe != nil {
		go r.pollProbe(ctx, "Ready", r.spec.ReadinessProbe)
	}
	if r.spec.LivenessProbe != nil {
		go r.pollProbe(ctx, "Healthy", r.spec.LivenessProbe)
	}
	<-ctx.Done()
}

func (r *Runner) pollProbe(ctx context.Context, condType string, p *impv1alpha1.Probe) {
	period := time.Duration(p.PeriodSeconds) * time.Second
	if period <= 0 {
		period = 10 * time.Second
	}

	failureThreshold := p.FailureThreshold
	if failureThreshold <= 0 {
		failureThreshold = 3
	}

	ticker := time.NewTicker(period)
	defer ticker.Stop()

	var consecutiveFails, consecutiveSuccesses int32

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok := r.runProbe(ctx, p)
			if ok {
				consecutiveFails = 0
				consecutiveSuccesses++
				// Patch only on transition to success (== 1) to avoid noisy repeated API calls.
				if consecutiveSuccesses == 1 {
					r.patcher([]metav1.Condition{probeCondition(condType, true)})
				}
			} else {
				consecutiveSuccesses = 0
				consecutiveFails++
				if consecutiveFails >= failureThreshold {
					r.patcher([]metav1.Condition{probeCondition(condType, false)})
				}
			}
		}
	}
}

func (r *Runner) runProbe(ctx context.Context, p *impv1alpha1.Probe) bool {
	const defaultTimeout = 5 * time.Second

	pCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	if p.Exec != nil {
		resp, err := r.client.Exec(pCtx, &pb.ExecRequest{
			Command:        p.Exec.Command,
			TimeoutSeconds: int32(defaultTimeout.Seconds()),
		})
		return err == nil && resp.ExitCode == 0
	}
	if p.HTTP != nil {
		resp, err := r.client.HTTPCheck(pCtx, &pb.HTTPCheckRequest{
			Port:           p.HTTP.Port,
			Path:           p.HTTP.Path,
			TimeoutSeconds: int32(defaultTimeout.Seconds()),
		})
		return err == nil && resp.StatusCode >= 200 && resp.StatusCode < 400
	}
	return false
}

func probeCondition(condType string, ready bool) metav1.Condition {
	status := metav1.ConditionTrue
	reason := "ProbeSucceeded"
	msg := "probe passed"
	if !ready {
		status = metav1.ConditionFalse
		reason = "ProbeFailed"
		msg = "probe failed"
	}
	return metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	}
}
