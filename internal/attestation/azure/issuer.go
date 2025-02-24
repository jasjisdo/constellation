/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package azure

import (
	"github.com/edgelesssys/constellation/internal/atls"
	"github.com/edgelesssys/constellation/internal/attestation/azure/snp"
	"github.com/edgelesssys/constellation/internal/attestation/azure/trustedlaunch"
	"github.com/edgelesssys/constellation/internal/attestation/vtpm"
)

// NewIssuer returns an SNP issuer if it can successfully read the idkeydigest from the TPM.
// Otherwise returns a Trusted Launch issuer.
func NewIssuer() atls.Issuer {
	if _, err := snp.GetIdKeyDigest(vtpm.OpenVTPM); err == nil {
		return snp.NewIssuer()
	} else {
		return trustedlaunch.NewIssuer()
	}
}
