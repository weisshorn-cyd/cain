package utils

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	ErrNoLogger               = errors.New("logger cannot be nil")
	ErrNoMetrics              = errors.New("metrics cannot be nil")
	ErrMisformattedSecretName = errors.New("malformatted secret name")
	ErrEmptyNamespace         = errors.New("namespace is empty")
)

type CASecret struct {
	name string
	keys []string
}

const (
	expectedCAParts = 2
)

func (cs *CASecret) UnmarshalText(text []byte) error {
	caSecretSplit := strings.Split(string(text), "/")
	if len(caSecretSplit) != expectedCAParts {
		return ErrMisformattedSecretName
	}

	secretName := caSecretSplit[0]
	secretValue := caSecretSplit[1]

	secretDataKeys := strings.Split(secretValue, ",")

	*cs = CASecret{
		name: secretName,
		keys: secretDataKeys,
	}

	return nil
}

func (cs *CASecret) Name() string {
	return cs.name
}

func (cs *CASecret) Keys() []string {
	return cs.keys
}

// GetPodNS reads the K8s serviceaccount files to find the Pods namespace.
func GetPodNS() (string, error) {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", fmt.Errorf("reading service account namespace file: %w", err)
	}

	ns := strings.TrimSpace(string(data))
	if len(ns) < 1 {
		return ns, ErrEmptyNamespace
	}

	return ns, nil
}
