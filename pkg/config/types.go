package config

import "time"

const (
	APIVersion = "navarch.io/v1alpha1"

	KindControlPlane = "ControlPlane"
	KindPool         = "Pool"
	KindProvider     = "Provider"
)

// TypeMeta describes the API version and kind of a resource.
type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
}

// ObjectMeta contains metadata that all resources have.
type ObjectMeta struct {
	Name        string            `yaml:"name" json:"name"`
	Namespace   string            `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// ControlPlane configures the Navarch control plane.
type ControlPlane struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta         `yaml:"metadata" json:"metadata"`
	Spec     ControlPlaneSpec   `yaml:"spec" json:"spec"`
	Status   *ControlPlaneStatus `yaml:"status,omitempty" json:"status,omitempty"`
}

// ControlPlaneSpec defines the desired state of the control plane.
type ControlPlaneSpec struct {
	Address string `yaml:"address,omitempty" json:"address,omitempty"` // Listen address (default: :50051)

	// Node configuration defaults
	HealthCheckInterval Duration `yaml:"healthCheckInterval,omitempty" json:"healthCheckInterval,omitempty"`
	HeartbeatInterval   Duration `yaml:"heartbeatInterval,omitempty" json:"heartbeatInterval,omitempty"`
	EnabledHealthChecks []string `yaml:"enabledHealthChecks,omitempty" json:"enabledHealthChecks,omitempty"`

	// Pool management
	AutoscaleInterval Duration `yaml:"autoscaleInterval,omitempty" json:"autoscaleInterval,omitempty"`

	// TLS configuration
	TLS *TLSConfig `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// ControlPlaneStatus represents the observed state of the control plane.
type ControlPlaneStatus struct {
	Ready       bool   `yaml:"ready" json:"ready"`
	NodeCount   int    `yaml:"nodeCount" json:"nodeCount"`
	PoolCount   int    `yaml:"poolCount" json:"poolCount"`
	LastUpdated string `yaml:"lastUpdated,omitempty" json:"lastUpdated,omitempty"`
}

// TLSConfig holds TLS settings.
type TLSConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	CertFile   string `yaml:"certFile,omitempty" json:"certFile,omitempty"`
	KeyFile    string `yaml:"keyFile,omitempty" json:"keyFile,omitempty"`
	CAFile     string `yaml:"caFile,omitempty" json:"caFile,omitempty"`
	SecretName string `yaml:"secretName,omitempty" json:"secretName,omitempty"` // For K8s secret reference
}

// Pool configures a GPU node pool.
type Pool struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta `yaml:"metadata" json:"metadata"`
	Spec     PoolSpec   `yaml:"spec" json:"spec"`
	Status   *PoolStatus `yaml:"status,omitempty" json:"status,omitempty"`
}

// PoolSpec defines the desired state of a pool.
type PoolSpec struct {
	ProviderRef  string            `yaml:"providerRef" json:"providerRef"`   // Name of Provider resource
	InstanceType string            `yaml:"instanceType" json:"instanceType"` // Cloud instance type
	Region       string            `yaml:"region" json:"region"`
	Zones        []string          `yaml:"zones,omitempty" json:"zones,omitempty"`
	SSHKeyNames  []string          `yaml:"sshKeyNames,omitempty" json:"sshKeyNames,omitempty"`
	Labels       map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"` // Labels applied to nodes

	Scaling ScalingSpec `yaml:"scaling" json:"scaling"`
	Health  HealthSpec  `yaml:"health,omitempty" json:"health,omitempty"`
}

// ScalingSpec configures pool scaling behavior.
type ScalingSpec struct {
	MinReplicas    int      `yaml:"minReplicas" json:"minReplicas"`
	MaxReplicas    int      `yaml:"maxReplicas" json:"maxReplicas"`
	CooldownPeriod Duration `yaml:"cooldownPeriod,omitempty" json:"cooldownPeriod,omitempty"`

	Autoscaler *AutoscalerSpec `yaml:"autoscaler,omitempty" json:"autoscaler,omitempty"`
}

// AutoscalerSpec configures the autoscaling strategy.
type AutoscalerSpec struct {
	Type string `yaml:"type" json:"type"` // reactive, queue, scheduled, predictive, composite

	// Reactive autoscaler
	ScaleUpThreshold   *float64 `yaml:"scaleUpThreshold,omitempty" json:"scaleUpThreshold,omitempty"`
	ScaleDownThreshold *float64 `yaml:"scaleDownThreshold,omitempty" json:"scaleDownThreshold,omitempty"`

	// Queue-based autoscaler
	JobsPerNode *int `yaml:"jobsPerNode,omitempty" json:"jobsPerNode,omitempty"`

	// Scheduled autoscaler
	Schedule []ScheduleEntry  `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	Fallback *AutoscalerSpec `yaml:"fallback,omitempty" json:"fallback,omitempty"`

	// Predictive autoscaler
	LookbackWindow *int     `yaml:"lookbackWindow,omitempty" json:"lookbackWindow,omitempty"`
	GrowthFactor   *float64 `yaml:"growthFactor,omitempty" json:"growthFactor,omitempty"`

	// Composite autoscaler
	Mode        string           `yaml:"mode,omitempty" json:"mode,omitempty"` // max, min, avg
	Autoscalers []AutoscalerSpec `yaml:"autoscalers,omitempty" json:"autoscalers,omitempty"`
}

// ScheduleEntry defines scaling limits for a time window.
type ScheduleEntry struct {
	DaysOfWeek  []string `yaml:"daysOfWeek,omitempty" json:"daysOfWeek,omitempty"`
	StartHour   int      `yaml:"startHour" json:"startHour"`
	EndHour     int      `yaml:"endHour" json:"endHour"`
	MinReplicas int      `yaml:"minReplicas" json:"minReplicas"`
	MaxReplicas int      `yaml:"maxReplicas" json:"maxReplicas"`
}

// HealthSpec configures health checking and remediation.
type HealthSpec struct {
	UnhealthyThreshold int  `yaml:"unhealthyThreshold,omitempty" json:"unhealthyThreshold,omitempty"`
	AutoReplace        bool `yaml:"autoReplace,omitempty" json:"autoReplace,omitempty"`
}

// PoolStatus represents the observed state of a pool.
type PoolStatus struct {
	Replicas        int    `yaml:"replicas" json:"replicas"`
	ReadyReplicas   int    `yaml:"readyReplicas" json:"readyReplicas"`
	HealthyReplicas int    `yaml:"healthyReplicas" json:"healthyReplicas"`
	Phase           string `yaml:"phase" json:"phase"` // Pending, Running, Scaling, Degraded
	LastScaleTime   string `yaml:"lastScaleTime,omitempty" json:"lastScaleTime,omitempty"`
}

// Provider configures a cloud provider.
type Provider struct {
	TypeMeta `yaml:",inline" json:",inline"`
	Metadata ObjectMeta   `yaml:"metadata" json:"metadata"`
	Spec     ProviderSpec `yaml:"spec" json:"spec"`
}

// ProviderSpec defines the provider configuration.
type ProviderSpec struct {
	Type string `yaml:"type" json:"type"` // lambda, gcp, aws, fake

	// Lambda Labs
	Lambda *LambdaProviderSpec `yaml:"lambda,omitempty" json:"lambda,omitempty"`

	// GCP
	GCP *GCPProviderSpec `yaml:"gcp,omitempty" json:"gcp,omitempty"`

	// AWS
	AWS *AWSProviderSpec `yaml:"aws,omitempty" json:"aws,omitempty"`

	// Fake (for development)
	Fake *FakeProviderSpec `yaml:"fake,omitempty" json:"fake,omitempty"`
}

// LambdaProviderSpec configures the Lambda Labs provider.
type LambdaProviderSpec struct {
	APIKeySecretRef *SecretRef `yaml:"apiKeySecretRef,omitempty" json:"apiKeySecretRef,omitempty"`
	APIKeyEnvVar    string     `yaml:"apiKeyEnvVar,omitempty" json:"apiKeyEnvVar,omitempty"` // Env var name containing API key
}

// GCPProviderSpec configures the GCP provider.
type GCPProviderSpec struct {
	Project              string     `yaml:"project" json:"project"`
	CredentialsSecretRef *SecretRef `yaml:"credentialsSecretRef,omitempty" json:"credentialsSecretRef,omitempty"`
}

// AWSProviderSpec configures the AWS provider.
type AWSProviderSpec struct {
	Region               string     `yaml:"region" json:"region"`
	CredentialsSecretRef *SecretRef `yaml:"credentialsSecretRef,omitempty" json:"credentialsSecretRef,omitempty"`
}

// FakeProviderSpec configures the fake provider for development.
type FakeProviderSpec struct {
	GPUCount int `yaml:"gpuCount,omitempty" json:"gpuCount,omitempty"` // GPUs per fake instance
}

// SecretRef references a secret.
type SecretRef struct {
	Name string `yaml:"name" json:"name"`
	Key  string `yaml:"key,omitempty" json:"key,omitempty"` // Key within the secret (default: same as field name)
}

// Duration wraps time.Duration for YAML/JSON marshaling.
type Duration time.Duration

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

