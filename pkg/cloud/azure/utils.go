package azure

import (
	"context"
	"fmt"
	"net/url"

	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/cloud"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/config"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/constants"
	"github.com/Obmondo/kubeaid-bootstrap-script/pkg/utils/assert"
)

// Type casts the give CloudProvider interface instance to an instance of the Azure struct.
// Panics if the type casting fails.
func CloudProviderToAzure(ctx context.Context, cloudProvider cloud.CloudProvider) *Azure {
	azure, ok := cloudProvider.(*Azure)
	assert.Assert(ctx, ok, "Failed type casting CloudProvider interface to Azure struct type")

	return azure
}

func GetServiceAccountIssuerURL(ctx context.Context) string {
	storageAccountURL := fmt.Sprintf(
		"https://%s.blob.core.windows.net/",
		config.ParsedGeneralConfig.Cloud.Azure.WorkloadIdentity.StorageAccountName,
	)

	serviceAccountIssuerURL, err := url.JoinPath(
		storageAccountURL,
		constants.BlobContainerNameWorkloadIdentity,
	)
	assert.AssertErrNil(ctx, err, "Failed constructing ServiceAccount issuer URL")

	return serviceAccountIssuerURL
}
