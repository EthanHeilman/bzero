package data

var (
	// KeyShard examples
	mockSplitPrivateKeyDefault = KeyEntry{
		KeyShardPem: "123",
		CaCertPem:   "",
	}

	mockSplitPrivateKeyAlt = KeyEntry{
		KeyShardPem: "101",
		CaCertPem:   "",
	}

	mockEntryDefault = MappedKeyEntry{
		KeyData:   mockSplitPrivateKeyDefault,
		TargetIds: []string{"targetId1", "targetId2"},
	}

	mockEntryDefaultPlusTarget = MappedKeyEntry{
		KeyData:   mockSplitPrivateKeyDefault,
		TargetIds: []string{"targetId1", "targetId2", "targetId3"},
	}

	mockEntryAlt = MappedKeyEntry{
		KeyData:   mockSplitPrivateKeyAlt,
		TargetIds: []string{"targetId1", "targetId2"},
	}
)

func DefaultMockKeyShardDataSmall() KeyShardData {
	return KeyShardData{Keys: []MappedKeyEntry{mockEntryDefault}}
}

func AltMockKeyShardDataSmall() KeyShardData {
	return KeyShardData{[]MappedKeyEntry{mockEntryAlt}}
}

func DefaultMockKeyEntry3Target() MappedKeyEntry {
	return MappedKeyEntry{
		KeyData:   mockSplitPrivateKeyDefault,
		TargetIds: []string{"targetId1", "targetId2", "targetId3"},
	}
}

func DefaultMockSplitPrivateKey() KeyEntry {
	return KeyEntry{
		KeyShardPem: "123",
		CaCertPem:   "",
	}
}

func AltMockSplitPrivateKey() KeyEntry {
	return KeyEntry{
		KeyShardPem: "101",
		CaCertPem:   "",
	}
}

func DefaultMockTargetIds() []string {
	return []string{"targetId1", "targetId2"}
}

func MockKeyShardDataMedium() KeyShardData {
	return KeyShardData{
		Keys: []MappedKeyEntry{
			mockEntryDefault,
			mockEntryAlt,
		},
	}
}

func MockKeyShardLargeNoTargetsRaw() string {
	return `
{
  "keys":
    [
      {
        "key": {
    		"keyShardPem": "1",
    		"caCertPem": ""
    	},
        "targetIds": []
      },
      {
        "key": {
    		"keyShardPem": "2",
    		"caCertPem": ""
    	},
        "targetIds": []
      },
      {
        "key": {
    		"keyShardPem": "3",
    		"caCertPem": ""
    	},
        "targetIds": []
      },
      {
        "key": {
    		"keyShardPem": "4",
    		"caCertPem": ""
    	},
        "targetIds": []
      }
    ]
}
`
}

func MockKeyShardLargeWithTargetsObject() KeyShardData {
	return KeyShardData{
		Keys: []MappedKeyEntry{
			{
				KeyData: KeyEntry{
					KeyShardPem: "1",
				},
				TargetIds: []string{"targetId0", "targetId1"},
			},
			{
				KeyData: KeyEntry{
					KeyShardPem: "2",
				},
				TargetIds: []string{"targetId2", "targetId3"},
			},
			{
				KeyData: KeyEntry{
					KeyShardPem: "3",
				},
				TargetIds: []string{"targetId4", "targetId5"},
			},
			{
				KeyData: KeyEntry{
					KeyShardPem: "4",
				},
				TargetIds: []string{"targetId6", "targetId7"},
			},
		},
	}
}

func MockKeyShardLargeWithTargetsRaw() string {
	return `
{
  "keys":
    [
      {
        "key": {
    		"keyShardPem": "1",
    		"caCertPem": ""
    	},
        "targetIds": [
          "targetId0",
          "targetId1"
        ]
      },
      {
        "key": {
    		"keyShardPem": "2",
    		"caCertPem": ""
    	},
        "targetIds": [
          "targetId2",
          "targetId3"
        ]
      },
      {
        "key": {
    		"keyShardPem": "3",
    		"caCertPem": ""
    	},
        "targetIds": [
          "targetId4",
          "targetId5"
        ]
      },
      {
        "key": {
    		"keyShardPem": "4",
    		"caCertPem": ""
    	},
        "targetIds": [
          "targetId6",
          "targetId7"
        ]
      }
    ]
}
`
}

func HugeKeyPem() string {
	return `-----BEGIN RSA SPLIT PRIVATE KEY-----
MIIJszCCBNoEggTRODEzNzYwMzU1MDc5ODk1NDkwMzQzNTY4MDU4NDA5NDgzODEz
MjMzNTEwMDM0Mzc1MTIyMDc2MzIwNzY2NTA4NTUzNDAwNDA0NDU3MTkwNzM5NDk0
MTUyMDE2ODk3MzQyMjQ4OTQzNjI1MzE1NjI5NjU0NDkzMDQzMjQ2NDI4MTAzNTEw
MzgwNDUxNzU1MDEwNTg0MzAyMzY2MDAzNjEzOTc5NDAyNDg5ODE1OTA2MjU0MTIz
NzQ5MTA2OTE2NDcwOTQwNTA3MjMwMTAxMDkwNzg4MTE1OTE1Njk3OTgyNTg1Nzg2
NTc3MDYzNTA3MDI5NzA3NzY1NDczMDIxNjQyNzY2MDUzODU3NTg0NDIzNDk3Mjg1
MDE2MzEyNzc5ODg0ODgxNzM0OTk1OTU2NjM2MjYyNDI4NzQwNjU4NDc2MDM2ODQy
ODUzNDIxMTExODM3MDk4Mjg1MzAwMjc1NzY1MDMwNjI2MDYxMzM1NzU5MDQ3OTM1
NTU5NDMwNzczMzMyOTk4OTI4MjcyNzY0NDI3MDIzNTE2MzUxMDY1ODM1MDU4NzI2
NTgwMTAzNDgyODQ1Mzg3NjIxNjUyMDM3NDE2MTE2MzM3NjA0Mjk0NzMxOTcwMTU1
MzY3ODgzNjAzNzk2MjIyNzIxODM4NzA5MjIxNTc4Mjc3Mjk4MDc5MDY4MjU1NDI4
OTkyODc0MTE3NTQxMzM3NzM1NTE0NDQzNzAzOTU5MjI5MjYyNTc2NDQ1MTU4MTU0
NDAzMDgzNDg4Njg5MzUzODg1MDA2MTI4ODE0MjU1NjIyNjAwMzg1ODU1MzY2NjI5
NjI1NTIwNDE5MzA0NTE4NzYyODkxMzc4MzYzMzk0MTgwMzMxNTIyMTYwMjQ4NzI1
MjkzNDIyNDMzMDU4NDgwNDMwMjIyOTgyMDY3Nzc1NzgyMzU4NTU4OTA2ODYxODYx
MTc1NTA4NTI0MDE0NjIxMDc2MzgzOTMzNzE2NTg2MjAxMzAxMDAxODM4NTA4NTc2
NzU2MTQzMDk1NzI5NDA4MjUzMzU0NTkxNjc3NDQ0MDEwMjcxMTE2NjMwMDUwODg0
MTI2NDEwMjA5NTE1OTQ3NDIzMDIyMTczMzMzNDI5MTgwNjA0NzgxNjA4MDg4MzI4
NjIyMDQzMjkwMzU0NjUxMTY1NTE0MDY0MzYxNTY0NjgzMjA3NTU5MzE0NTYwNjcz
OTE0MjM4MzIzNjAxMjI5ODgwODQ2NTY1MDc5NjI2NjE3NDA1MzE4MjI1MTgwMTA5
NDkxNjQxOTY0NjI3MjQ5NzE4MTAzNTQ0MjE4MzM1MTU5MzQ5NzE4NjQzMTkyOTky
MjkzMTY0MTI3MDc0MTQ0NzM5ODAwNzkzNTczNTEyODc2ODgwMTc0MzQ0NTMwMjI4
NDk4OTQwODczNTIxMjc1MDE3OTgwNDc5NTI0NjI4NzY0MjIxMzcxNzE2NTIwODIy
ODU1NTkxMzE0NDkzMzE5NzY3MzY3NDMxMzk4OTMxMTE5ODUxOTc3NTA2NzI5NTQz
MzEyMDQ3OTUxODk5NjA0NDM5MDMwMDE5NDkxMjYwMDgxMDQwOTkwOTM4OTY3NzE2
MTU5MzU4MzkyMjI3MzM0MzY1MDM5MzcyMTk4OTg2MjY4MDk2MjA5NDU5ODQ5AgMB
AAEEggTRNDYxNTc5OTY4OTk1MzAyMjA2OTAzNjY4NzY3NzA3MjEwOTY0NzM1MDg1
NDkyNTIyNjM1NjI4OTU3MTk4OTU3NTgzMTExNDU0MjQ0OTQwMTgzNzcxNjc0NjAx
NzI1MzY0MTgwOTEyOTQyMjMxMDY1NDU0NjY4MzA3Mzc4MjU1ODQzMTU0Mjk3MDUx
NTU1NzU4MzIwOTk0NzU2ODE2MDIwNTM5MTU4NzM1OTA5MjQ3NDgyNzU5ODA2MjY4
MDkzNzUxODk1NzMxMjkxMTk3NTQ1NzkxMTY1MDkwOTM5OTMyMDE1NjQwMDMxNjU0
NjkyMjM5NjU1ODQ0NzQwNjA1NzM2ODIxMDkyOTY3MTc1MjczMDM5ODc3MjI2MTQ0
NDMyMzQ3MjA2NTMwMjg0NTk0NzA4NzAyOTkxNTg1MDg1MTg4NTc1NDA0MDcyNjQ1
Mzg2NzcxMDkyMjQ1OTI2OTIxMDUzNDgxNzY3NDAyODM4ODI2MDYyOTU3OTgzOTY1
NTM4NDkyMDYzNjk4NzMzNjU5Mzg2MDk3NTM5NjE3ODczMzc0MjU2MTc2Mjc2ODI2
OTM2MTA5MzY5NzUwNzY1Nzc2ODIxNjQxNjkzMTgwOTgwMzczNzczNzUyMDcyMTYy
NjkxNDIzMzIyMTU3NDI4NDQ5NjA3MDU3Njc5MzcwODYxNTIwMzcwNzU5Mjk2MDk2
NzcyMjgyMjg1NDIyOTkyNDE1NDk0OTEzOTQ1NzAwNjkxOTU0MTUzNzU4OTgwOTA5
MDc4MzY0NDEyNzc4OTM4NDAyMTY1NjY2NjMwMTAxNTg4ODk0NTIwNzM5MzEwMTU2
MDU4MTYwNjU0MzU2NjA2ODMzMTAxMzg0MDgxNjY1MjI1OTM2MTg3ODU3ODYxODky
MzI3MzE3OTI4ODA5NDEyNzQyODkwMjgwNTk0Nzc0ODYwNDg1MDM1ODE2MDg3MTEw
NTA5ODU0NTg2NTQwMDU0NjkwMDMwNTAwNTUwNjMwNjQ0NDU3MjE1MjkwODI3MjAw
MTE1MTg4MjI0NzAxNTc0MDY2ODMxMjc3NTI3Mzk2NjE1NDk5MjUwMTk4MzEwOTE3
NzAwMzcyNTYyNzQ0MzU0NjQzOTgyMjc3OTgxMTk5MTMyNTU5OTE2NTA4MDU5NjA0
NTc3NTQ3NDUxMzc5OTk1NDQxNDMzOTg3NTUwMTQ1NTY5MTkyOTc2NTMzODE1MjY0
NTUxNTAxNTI5ODI3MTEzNTgzMTAyNDYyMTAyMTk1MTE5NTA3MjAwMzkyNDczMTgw
NDI0NTU2MzA3OTgxMTYwODAwMDkzNTAzNTQ1MjMzMTU2NzIzNTMwMTg1MTE3MjY1
MTc1MTI4Njc0NDk3NDc1NzU3NDQxMTE3MDM1OTc3OTM4OTg0MjMxMTM4NzYzMjA0
NzkzMjAzNjU3NDMwMDI2MzAzODE3MTk4MjQzMjE4MjYyMzYxOTA0OTc4ODA2ODQz
MjI4ODY2OTIxOTM3MjI4MDYxMTk3OTE0MzIyODYyNTA5NzE3Mjk0NDAwNzQzNjY3
NTg1NzQ5MTMzNTc4MTUzMTMxNjQ3NTIwMDYyNTc5ODQ1NTIwMTQ2ODgwNDk2OTYy
MzkxMTI0MTYwNzA2NDg0NDczODYxMTg5MzkyMjE2NjUyNDI5NDA3
-----END RSA SPLIT PRIVATE KEY-----
`
}
