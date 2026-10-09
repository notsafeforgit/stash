package models

import (
	"context"
	"time"
)

// WorkerPolicyUpgrade approves a compatible executable repair for one exact
// metadata profile. Original job/listing definitions remain immutable. Source
// coverage, grants, review holds and retry deadlines are not changed.
type WorkerPolicyUpgradeInput struct {
	RequestUUID          string `json:"request_uuid"`
	Kind                 string `json:"kind"`
	OriginalPolicySHA256 string `json:"original_policy_sha256"`
	ExpectedPolicySHA256 string `json:"expected_policy_sha256"`
	PolicySHA256         string `json:"policy_sha256"`
	Reason               string `json:"reason"`
}

type WorkerPolicyUpgrade struct {
	WorkerPolicyUpgradeInput
	CreatedAt time.Time `json:"created_at"`
}

type WorkerPolicyReaderWriter interface {
	Resolve(context.Context, string, string) (string, error)
	Upgrade(context.Context, WorkerPolicyUpgradeInput, time.Time) (*WorkerPolicyUpgrade, error)
	Receipt(context.Context, string) (*WorkerPolicyUpgrade, error)
}
