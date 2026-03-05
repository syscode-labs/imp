/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	impdevv1alpha1 "github.com/syscode-labs/imp/api/v1alpha1"
)

func TestComputeBackoffDelay(t *testing.T) {
	tests := []struct {
		name         string
		policy       impdevv1alpha1.RestartBackoff
		restartCount int32
		want         time.Duration
	}{
		{
			name:         "first attempt uses initialDelay",
			policy:       impdevv1alpha1.RestartBackoff{MaxRetries: 5, InitialDelay: "10s", MaxDelay: "5m"},
			restartCount: 0,
			want:         10 * time.Second,
		},
		{
			name:         "second attempt doubles",
			policy:       impdevv1alpha1.RestartBackoff{MaxRetries: 5, InitialDelay: "10s", MaxDelay: "5m"},
			restartCount: 1,
			want:         20 * time.Second,
		},
		{
			name:         "third attempt doubles again",
			policy:       impdevv1alpha1.RestartBackoff{MaxRetries: 5, InitialDelay: "10s", MaxDelay: "5m"},
			restartCount: 2,
			want:         40 * time.Second,
		},
		{
			name:         "capped at maxDelay",
			policy:       impdevv1alpha1.RestartBackoff{MaxRetries: 5, InitialDelay: "10s", MaxDelay: "30s"},
			restartCount: 10,
			want:         30 * time.Second,
		},
		{
			name:         "empty fields use defaults",
			policy:       impdevv1alpha1.RestartBackoff{},
			restartCount: 0,
			want:         10 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeBackoffDelay(tt.policy, tt.restartCount)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestShouldRestart(t *testing.T) {
	policy := &impdevv1alpha1.RestartPolicy{
		Mode:         "in-place",
		Backoff:      impdevv1alpha1.RestartBackoff{MaxRetries: 3},
		OnExhaustion: "fail",
	}

	assert.True(t, shouldRestart(policy, 0))
	assert.True(t, shouldRestart(policy, 2))
	assert.False(t, shouldRestart(policy, 3))  // at limit — exhausted
	assert.False(t, shouldRestart(policy, 10)) // over limit
	assert.False(t, shouldRestart(nil, 0))     // nil policy
}

func TestShouldRestart_defaultMaxRetries(t *testing.T) {
	// MaxRetries=0 should use default of 5
	policy := &impdevv1alpha1.RestartPolicy{
		Backoff: impdevv1alpha1.RestartBackoff{MaxRetries: 0},
	}
	assert.True(t, shouldRestart(policy, 4))
	assert.False(t, shouldRestart(policy, 5))
}

func TestShouldCoolDownReset(t *testing.T) {
	// 2h ago, coolDown 1h → should reset
	exhausted := metav1.NewTime(time.Now().Add(-2 * time.Hour))
	assert.True(t, shouldCoolDownReset("1h", &exhausted))

	// 30min ago, coolDown 1h → should NOT reset
	recent := metav1.NewTime(time.Now().Add(-30 * time.Minute))
	assert.False(t, shouldCoolDownReset("1h", &recent))

	// nil exhaustedAt → false
	assert.False(t, shouldCoolDownReset("1h", nil))

	// empty period → defaults to 1h; exhausted 2h ago → reset
	assert.True(t, shouldCoolDownReset("", &exhausted))
}
