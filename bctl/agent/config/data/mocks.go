package data

import (
	"fmt"

	"bastionzero.com/bctl/v1/bzerolib/keypair"

	// we can't import ginkgo here since it adds Ginkgo's help options to bzero's
	. "github.com/onsi/gomega"
)

const (
	mockVersion            = "fakeVersion"
	mockAgentType          = "fakeAgentType"
	mockServiceUrl         = "http://hasthelargehadroncolliderdestroyedtheworldyet.com/"
	mockAgentIdentityToken = "faketoken"
	mockTargetId           = "fakeTargetId"
	mockIdpProvider        = "fakeIdpProvider"
	mockIdpOrgId           = "fakeIdpOrgId"
	mockShutdownReason     = "fakeReason"

	// Only used by V1
	mockNamespaceV1       = "fakeNamespace"
	mockTargetNameV1      = "fakeTargetName"
	mockEnvironmentIdV1   = "fakeEnvironmentId"
	mockEnvironmentNameV1 = "fakeEnvironmentName"
)

var (
	mockPublickey, mockPrivatekey, _ = keypair.GenerateKeyPair()
	mockShutdownState                = map[string]string{
		"fake":     "garbage",
		"morefake": "moregarbage",
	}

	// Only used by V1
	mockShutdownStateV1 = fmt.Sprintf("%+v", mockShutdownState)

	// KeyShard examples
	mockSplitPrivateKeyDefault = SplitPrivateKey{
		D: []byte("123"),
		E: []byte("45"),
		PublicKey: PublicKey{
			N: []byte("678"),
			E: 90,
		},
	}

	mockSplitPrivateKeyAlt = SplitPrivateKey{
		D: []byte("101"),
		E: []byte("202"),
		PublicKey: PublicKey{
			N: []byte("303"),
			E: 404,
		},
	}

	mockEntryDefault = KeyEntry{
		Key:       mockSplitPrivateKeyDefault,
		TargetIds: []string{"targetId1", "targetId2"},
	}

	mockEntryDefaultPlusTarget = KeyEntry{
		Key:       mockSplitPrivateKeyDefault,
		TargetIds: []string{"targetId1", "targetId2", "targetId3"},
	}

	mockEntryAlt = KeyEntry{
		Key:       mockSplitPrivateKeyAlt,
		TargetIds: []string{"targetId1", "targetId2"},
	}
)

func NewMockDataV1() AgentDataV1 {
	return AgentDataV1{
		Version:            mockVersion,
		PublicKey:          mockPublickey.String(),
		PrivateKey:         mockPrivatekey.String(),
		AgentType:          mockAgentType,
		ServiceUrl:         mockServiceUrl,
		TargetId:           mockTargetId,
		AgentIdentityToken: mockAgentIdentityToken,
		IdpProvider:        mockIdpProvider,
		IdpOrgId:           mockIdpOrgId,
		ShutdownReason:     mockShutdownReason,
		ShutdownState:      mockShutdownStateV1,
		Namespace:          mockNamespaceV1,
		TargetName:         mockTargetNameV1,
		EnvironmentId:      mockEnvironmentIdV1,
		EnvironmentName:    mockEnvironmentNameV1,
	}
}

func (mockV1 *AgentDataV1) AssertMatchesV2(v2Data AgentDataV2) {
	// Since shutdown state has changed, we make sure that it's empty here
	// making sure the shutdown state is empty
	Expect(v2Data.ShutdownState).To(Equal(map[string]string{}))

	// matching all remaining fields are parsed verbatim
	Expect(v2Data.Version).To(Equal(mockV1.Version), fmt.Sprintf(`"%s" != "%s"`, v2Data.Version, mockV1.Version))
	Expect(v2Data.PublicKey.String()).To(Equal(mockV1.PublicKey), fmt.Sprintf(`"%s" != "%s"`, v2Data.PublicKey.String(), mockV1.PublicKey))
	Expect(v2Data.PrivateKey.String()).To(Equal(mockV1.PrivateKey), fmt.Sprintf(`"%s" != "%s"`, v2Data.PrivateKey.String(), mockV1.PrivateKey))
	Expect(v2Data.AgentType).To(Equal(mockV1.AgentType), fmt.Sprintf(`"%s" != "%s"`, v2Data.AgentType, mockV1.AgentType))
	Expect(v2Data.ServiceUrl).To(Equal(mockV1.ServiceUrl), fmt.Sprintf(`"%s" != "%s"`, v2Data.ServiceUrl, mockV1.ServiceUrl))
	Expect(v2Data.TargetId).To(Equal(mockV1.TargetId), fmt.Sprintf(`"%s" != "%s"`, v2Data.TargetId, mockV1.TargetId))
	Expect(v2Data.AgentIdentityToken).To(Equal(mockV1.AgentIdentityToken), fmt.Sprintf(`"%s" != "%s"`, v2Data.AgentIdentityToken, mockV1.AgentIdentityToken))
	Expect(v2Data.IdpProvider).To(Equal(mockV1.IdpProvider), fmt.Sprintf(`"%s" != "%s"`, v2Data.IdpProvider, mockV1.IdpProvider))
	Expect(v2Data.IdpOrgId).To(Equal(mockV1.IdpOrgId), fmt.Sprintf(`"%s" != "%s"`, v2Data.IdpOrgId, mockV1.IdpOrgId))
	Expect(v2Data.ShutdownReason).To(Equal(mockV1.ShutdownReason), fmt.Sprintf(`"%s" != "%s"`, v2Data.ShutdownReason, mockV1.ShutdownReason))
}

func NewMockDataV2() AgentDataV2 {
	return AgentDataV2{
		Version:            mockVersion,
		PublicKey:          mockPublickey,
		PrivateKey:         mockPrivatekey,
		AgentType:          mockAgentType,
		ServiceUrl:         mockServiceUrl,
		TargetId:           mockTargetId,
		AgentIdentityToken: mockAgentIdentityToken,
		IdpProvider:        mockIdpProvider,
		IdpOrgId:           mockIdpOrgId,
		ShutdownReason:     mockShutdownReason,
		ShutdownState:      mockShutdownState,
	}
}

func (mockV2 *AgentDataV2) AssertMatchesV2(v2Data AgentDataV2) {
	//making sure all fields are parsed verbatim
	Expect(v2Data.Version).To(Equal(mockV2.Version), fmt.Sprintf(`"%s" != "%s"`, v2Data.Version, mockV2.Version))
	Expect(v2Data.PublicKey.String()).To(Equal(mockV2.PublicKey.String()), fmt.Sprintf(`"%s" != "%s"`, v2Data.PublicKey.String(), mockV2.PublicKey.String()))
	Expect(v2Data.PrivateKey.String()).To(Equal(mockV2.PrivateKey.String()), fmt.Sprintf(`"%s" != "%s"`, v2Data.PrivateKey.String(), mockV2.PrivateKey.String()))
	Expect(v2Data.AgentType).To(Equal(mockV2.AgentType), fmt.Sprintf(`"%s" != "%s"`, v2Data.AgentType, mockV2.AgentType))
	Expect(v2Data.ServiceUrl).To(Equal(mockV2.ServiceUrl), fmt.Sprintf(`"%s" != "%s"`, v2Data.ServiceUrl, mockV2.ServiceUrl))
	Expect(v2Data.TargetId).To(Equal(mockV2.TargetId), fmt.Sprintf(`"%s" != "%s"`, v2Data.TargetId, mockV2.TargetId))
	Expect(v2Data.AgentIdentityToken).To(Equal(mockV2.AgentIdentityToken), fmt.Sprintf(`"%s" != "%s"`, v2Data.AgentIdentityToken, mockV2.AgentIdentityToken))
	Expect(v2Data.IdpProvider).To(Equal(mockV2.IdpProvider), fmt.Sprintf(`"%s" != "%s"`, v2Data.IdpProvider, mockV2.IdpProvider))
	Expect(v2Data.IdpOrgId).To(Equal(mockV2.IdpOrgId), fmt.Sprintf(`"%s" != "%s"`, v2Data.IdpOrgId, mockV2.IdpOrgId))
	Expect(v2Data.ShutdownReason).To(Equal(mockV2.ShutdownReason), fmt.Sprintf(`"%s" != "%s"`, v2Data.ShutdownReason, mockV2.ShutdownReason))
	Expect(v2Data.ShutdownState).To(Equal(mockV2.ShutdownState), fmt.Sprintf(`"%s" != "%s"`, v2Data.ShutdownState, mockV2.ShutdownState))
}

func DefaultMockKeyShardDataSmall() KeyShardData {
	return []KeyEntry{mockEntryDefault}
}

func AltMockKeyShardDataSmall() KeyShardData {
	return []KeyEntry{mockEntryAlt}
}

func DefaultMockKeyEntry3Target() KeyEntry {
	return KeyEntry{
		Key:       mockSplitPrivateKeyDefault,
		TargetIds: []string{"targetId1", "targetId2", "targetId3"},
	}
}

func DefaultMockSplitPrivateKey() SplitPrivateKey {
	return SplitPrivateKey{
		D: []byte("123"),
		E: []byte("45"),
		PublicKey: PublicKey{
			N: []byte("678"),
			E: 90,
		},
	}
}

func AltMockSplitPrivateKey() SplitPrivateKey {
	return SplitPrivateKey{
		D: []byte("101"),
		E: []byte("202"),
		PublicKey: PublicKey{
			N: []byte("303"),
			E: 404,
		},
	}
}

func DefaultMockTargetIds() []string {
	return []string{"targetId1", "targetId2"}
}

func MockKeyShardDataMedium() KeyShardData {
	return []KeyEntry{
		mockEntryDefault,
		mockEntryAlt,
	}
}

func MockKeyShardLargeNoTargetsRaw() string {
	return `
[
  {
    "key": {
      "d": "MQ==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": []
  },
  {
    "key": {
      "d": "Mg==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": []
  },
  {
    "key": {
      "d": "Mw==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": []
  },
  {
    "key": {
      "d": "NA==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": []
  }
]
`
}

func MockKeyShardLargeWithTargetsRaw() string {
	return `
[
  {
    "key": {
      "d": "MQ==",
      "e": "MjAy",
      "associatedPublicKey": {
        "n": "MzAz",
        "e": 404
      }
    },
    "targetIds": [
      "targetId0",
      "targetId1"
    ]
  },
  {
    "key": {
      "d": "Mg==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": [
      "targetId2",
      "targetId3"
    ]
  },
  {
    "key": {
      "d": "Mw==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": [
      "targetId4",
      "targetId5"
    ]
  },
  {
    "key": {
      "d": "NA==",
      "e": "NDU=",
      "associatedPublicKey": {
        "n": "Njc4",
        "e": 90
      }
    },
    "targetIds": [
      "targetId6",
      "targetId7"
    ]
  }
]
`
}
