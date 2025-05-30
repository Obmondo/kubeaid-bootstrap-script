package config

import (
	coreV1 "k8s.io/api/core/v1"
)

type (
	GeneralConfig struct {
		CustomerID string           `yaml:"customerID"`
		Git        GitConfig        `yaml:"git"`
		Cluster    ClusterConfig    `yaml:"cluster"    validate:"required"`
		Forks      ForksConfig      `yaml:"forkURLs"   validate:"required"`
		Cloud      CloudConfig      `yaml:"cloud"      validate:"required"`
		Monitoring MonitoringConfig `yaml:"monitoring"`
	}

	GitConfig struct {
		CABundlePath string `yaml:"caBundlePath"`
		CABundle     []byte `yaml:"caBundle"`

		UseSSHAgentAuth bool `yaml:"useSSHAgentAuth"`
	}

	ForksConfig struct {
		KubeaidForkURL       string `yaml:"kubeaid"       default:"https://github.com/Obmondo/KubeAid"`
		KubeaidConfigForkURL string `yaml:"kubeaidConfig"                                              validate:"notblank"`
	}

	ClusterConfig struct {
		Name           string `yaml:"name"           validate:"notblank"`
		K8sVersion     string `yaml:"k8sVersion"     validate:"notblank"`
		KubeaidVersion string `yaml:"kubeaidVersion" validate:"notblank"`

		EnableAuditLogging bool `yaml:"enableAuditLogging" default:"True"`

		APIServer APIServerConfig `yaml:"apiServer"`

		AdditionalUsers []UserConfig `yaml:"additionalUsers"`
	}

	/*
		REFER : https://github.com/kubernetes-sigs/cluster-api/blob/main/controlplane/kubeadm/config/crd/bases/controlplane.cluster.x-k8s.io_kubeadmcontrolplanes.yaml.

		NOTE : Generally, refer to the KubeadmControlPlane CRD instead of the corresponding GoLang
		       source types linked below.
		       There are some configuration options which appear in the corresponding GoLang source
		       type, but not in the CRD. If you set those fields, then they get removed by the Kubeadm
		       control-plane provider. This causes the capi-cluster ArgoCD App to always be in an
		       OutOfSync state, resulting to the KubeAid Bootstrap Script not making any progress!
	*/
	APIServerConfig struct {
		ExtraArgs    map[string]string     `yaml:"extraArgs"    default:"{}"`
		ExtraVolumes []HostPathMountConfig `yaml:"extraVolumes" default:"[]"`
		Files        []FileConfig          `yaml:"files"        default:"[]"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".HostPathMount
	HostPathMountConfig struct {
		Name      string              `yaml:"name"      validate:"notblank"`
		HostPath  string              `yaml:"hostPath"  validate:"notblank"`
		MountPath string              `yaml:"mountPath" validate:"notblank"`
		PathType  coreV1.HostPathType `yaml:"pathType"  validate:"required"`

		/*
			Whether the mount should be read-only or not.
			Defaults to true.

			NOTE : If you want the mount to be read-only, then set this true.
			       Otherwise, omit setting this field. It gets removed by the Kubeadm control-plane
			       provider component, which results to the capi-cluster ArgoCD App always being in
			       OutOfSync state.
		*/
		ReadOnly bool `yaml:"readOnly,omitempty"`
	}

	// REFER : "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1".File
	FileConfig struct {
		Path    string `yaml:"path"    validate:"notblank"`
		Content string `yaml:"content" validate:"notblank"`
	}

	UserConfig struct {
		Name         string `yaml:"name"         validate:"required"`
		SSHPublicKey string `yaml:"sshPublicKey" validate:"required"`
	}

	NodeGroup struct {
		Name string `yaml:"name" validate:"notblank"`

		CPU    uint32 `validate:"required"`
		Memory uint32 `validate:"required"`

		MinSize uint `yaml:"minSize" validate:"required"`
		Maxsize uint `yaml:"maxSize" validate:"required"`

		Labels map[string]string `yaml:"labels" default:"[]"`
		Taints []*coreV1.Taint   `yaml:"taints" default:"[]"`
	}

	CloudConfig struct {
		AWS     *AWSConfig     `yaml:"aws"`
		Hetzner *HetznerConfig `yaml:"hetzner"`
		Azure   *AzureConfig   `yaml:"azure"`
		Local   *LocalConfig   `yaml:"local"`

		DisasterRecovery *DisasterRecoveryConfig `yaml:"disasterRecovery"`
	}

	DisasterRecoveryConfig struct {
		VeleroBackupsBucketName        string `yaml:"veleroBackupsBucketName"        validate:"notblank"`
		SealedSecretsBackupsBucketName string `yaml:"sealedSecretsBackupsBucketName" validate:"notblank"`
	}

	SSHKeyPairConfig struct {
		PublicKeyFilePath string `yaml:"publicKeyFilePath" validate:"notblank"`
		PublicKey         string `                         validate:"notblank"`

		PrivateKeyFilePath string `yaml:"privateKeyFilePath" validate:"notblank"`
		PrivateKey         string `                          validate:"notblank"`
	}

	MonitoringConfig struct {
		KubePrometheusVersion string `yaml:"kubePrometheusVersion" default:"v0.14.0"`
		GrafanaURL            string `yaml:"grafanaURL"`
		ConnectObmondo        bool   `yaml:"connectObmondo"        default:"False"`
	}
)

// AWS specific.
type (
	AWSConfig struct {
		Region string `yaml:"region" validate:"notblank"`

		SSHKeyName     string          `yaml:"sshKeyName"     validate:"notblank"`
		VPCID          *string         `yaml:"vpcID"`
		BastionEnabled bool            `yaml:"bastionEnabled"                     default:"True"`
		ControlPlane   AWSControlPlane `yaml:"controlPlane"   validate:"required"`
		NodeGroups     []AWSNodeGroup  `yaml:"nodeGroups"     validate:"required"`
	}

	AWSControlPlane struct {
		LoadBalancerScheme string    `yaml:"loadBalancerScheme" default:"internet-facing" validate:"notblank"`
		Replicas           uint32    `yaml:"replicas"                                     validate:"required"`
		InstanceType       string    `yaml:"instanceType"                                 validate:"notblank"`
		AMI                AMIConfig `yaml:"ami"                                          validate:"required"`
	}

	AWSNodeGroup struct {
		NodeGroup `yaml:",inline"`

		AMI            AMIConfig `yaml:"ami"            validate:"required"`
		InstanceType   string    `yaml:"instanceType"   validate:"notblank"`
		RootVolumeSize uint32    `yaml:"rootVolumeSize" validate:"required"`
		SSHKeyName     string    `yaml:"sshKeyName"     validate:"notblank"`
	}

	AMIConfig struct {
		ID string `yaml:"id" validate:"notblank"`
	}
)

// Azure specific.
type (
	AzureConfig struct {
		TenantID       string         `yaml:"tenantID"       validate:"notblank"`
		SubscriptionID string         `yaml:"subscriptionID" validate:"notblank"`
		AADApplication AADApplication `yaml:"aadApplication" validate:"required"`
		Location       string         `yaml:"location"       validate:"notblank"`

		StorageAccount string `yaml:"storageAccount" validate:"notblank"`

		WorkloadIdentity WorkloadIdentity `yaml:"workloadIdentity" validate:"required"`

		SSHPublicKey string `yaml:"sshPublicKey" validate:"notblank"`

		ImageID *string `yaml:"imageID"`

		ControlPlane AzureControlPlane `yaml:"controlPlane" validate:"required"`
		NodeGroups   []AzureNodeGroup  `yaml:"nodeGroups"   validate:"required,gt=0"`
	}

	AADApplication struct {
		Name               string `yaml:"name"               validate:"notblank"`
		ObjectID           string `yaml:"objectID"           validate:"notblank"`
		ServicePrincipalID string `yaml:"servicePrincipalID" validate:"notblank"`
	}

	WorkloadIdentity struct {
		OpenIDProviderSSHKeyPair SSHKeyPairConfig `yaml:"openIDProviderSSHKeyPair" validate:"notblank"`
	}

	AzureControlPlane struct {
		LoadBalancerType string `yaml:"loadBalancerType" validate:"notblank"        default:"Public"`
		DiskSizeGB       uint32 `yaml:"diskSizeGB"       validate:"required,gt=100"`
		VMSize           string `yaml:"vmSize"           validate:"notblank"`
		Replicas         uint32 `yaml:"replicas"         validate:"required,gt=0"`
	}

	AzureNodeGroup struct {
		NodeGroup `yaml:",inline"`

		VMSize     string `yaml:"vmSize"     validate:"notblank"`
		DiskSizeGB uint32 `yaml:"diskSizeGB" validate:"required"`
	}
)

// Hetzner specific.
type (
	HetznerConfig struct {
		Mode string `yaml:"mode" default:"hcloud" validate:"notblank,oneof=bare-metal hcloud hybrid"`

		Zone   string `yaml:"zone"   validate:"notblank"`
		Region string `yaml:"region" validate:"notblank"`

		HCloudSSHKeyPairName string `yaml:"hcloudSSHKeyPairName" validate:"notblank"`

		NetworkEnabled bool   `yaml:"networkEnabled" default:"True"         validate:"required"`
		ImageName      string `yaml:"imageName"      default:"ubuntu-24.04" validate:"notblank"`

		ControlPlane HetznerControlPlane `yaml:"controlPlane" validate:"required"`
		NodeGroups   HetznerNodeGroups   `yaml:"nodeGroups"   validate:"required"`
	}

	HetznerControlPlane struct {
		MachineType  string                         `yaml:"machineType"  validate:"notblank"`
		Replicas     uint                           `yaml:"replicas"     validate:"required"`
		Regions      []string                       `yaml:"regions"      validate:"required,gt=0"`
		LoadBalancer HCloudControlPlaneLoadBalancer `yaml:"loadBalancer"`
	}

	HCloudControlPlaneLoadBalancer struct {
		Enabled bool   `yaml:"enabled" validate:"required"`
		Region  string `yaml:"region"  validate:"notblank"`
	}

	HetznerNodeGroups struct {
		HCloud []HCloudNodeGroup `yaml:"hcloud"`
	}

	HCloudNodeGroup struct {
		NodeGroup `yaml:",inline"`

		MachineType    string `yaml:"machineType" validate:"notblank"`
		RootVolumeSize uint32 `                   validate:"required"`
	}
)

// Local specific.
type (
	LocalConfig struct{}
)
