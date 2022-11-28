package userkeys

var mockTargetIds = []string{"targetId1", "targetId2"}

var oldSplitPrivateKey = SplitPrivateKey{
	D: int64(101),
	E: 202,
	PublicKey: PublicKey{
		N: int64(303),
		E: 404,
	},
}

var mockSplitPrivateKey = SplitPrivateKey{
	D: int64(123),
	E: 45,
	PublicKey: PublicKey{
		N: int64(678),
		E: 90,
	},
}

var mockEntry = KeyEntry{
	Key:       mockSplitPrivateKey,
	TargetIds: mockTargetIds,
}

var mockEntryAllTargets = KeyEntry{
	Key:       mockSplitPrivateKey,
	TargetIds: append([]string{"targetId0"}, mockTargetIds...),
}

var exampleInvalid = `
x: 1
y: 2
z: 3
`

var exampleSmall = `
- key: 
    d: 123
    e: 45
    associatedPublicKey: 
      e: 90
      n: 678
  targetIds: 
    - targetId1
    - targetId2
`

var exampleSmallOneTarget = `
- key: 
    d: 123
    e: 45
    associatedPublicKey: 
      e: 90
      n: 678
  targetIds: 
    - targetId1
`

var exampleMediumSomeTargets = `
- key:
    d: 101
    e: 202
    associatedPublicKey:
      n: 303
      e: 404
  targetIds: ["targetId0", "targetId1"]
- key:
    d: 123
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: ["targetId0", "targetId1"]
`

var exampleMediumAllTargets = `
- key:
    d: 101
    e: 202
    associatedPublicKey:
      n: 303
      e: 404
  targetIds: ["targetId0", "targetId1"]
- hash: hash-of-mock-key
  key:
  d: 123
  e: 45
  associatedPublicKey:
    n: 678
    e: 90
  targetIds: ["targetId0", "targetId1", "targetId2"]
`

var exampleLargeNoTargets = `
- key:
    d: 1
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: []
- key:
    d: 2
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: []
- key:
    d: 3
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: []
- key:
    d: 4
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: []
`

var exampleLargeWithTargets = `
- key:
    d: 1
    e: 202
    associatedPublicKey:
      n: 303
      e: 404
  targetIds: ["targetId0", "targetId1"]
- key:
    d: 2
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: ["targetId2", "targetId3"]
- key:
    d: 3
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: ["targetId4", "targetId5"]
- key:
    d: 4
    e: 45
    associatedPublicKey:
      n: 678
      e: 90
  targetIds: ["targetId6", "targetId7"]
`
