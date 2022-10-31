package envconfig

const exampleSmall = `
testKey1: 
  comment: this is an old value that will be replaced by the test
  value: FAKE_VALUE
  env: FAKE_VAR
`

const exampleMedium = `
testKey1: 
  comment: this is a correct value
  value: testValue-from-file
  env: TEST_ENV_VAR_1

testKey2: 
  comment: this is an old value that will be replaced by the test
  value: FAKE_VAL
  env: TEST_ENV_VAR_2
`

const exampleLarge = `
testKey1: 
  value: testValue-1
  env: TEST_ENV_VAR_1

testKey2:
  value: testValue-2
  env: TEST_ENV_VAR_2

testKey3: 
  value: testValue-3
  env: TEST_ENV_VAR_3

testKey4:
  value: testValue-4
  env: TEST_ENV_VAR_4

testKey5: 
  value: testValue-5
  env: TEST_ENV_VAR_5

testKey6:
  value: testValue-6
  env: TEST_ENV_VAR_6

testKey7: 
  value: testValue-7
  env: TEST_ENV_VAR_7

testKey8:
  value: testValue-8
  env: TEST_ENV_VAR_8

testKey9: 
  value: testValue-9
  env: TEST_ENV_VAR_9

testKey10:
  value: testValue-10
  env: TEST_ENV_VAR_10

testKey11: 
  value: testValue-11
  env: TEST_ENV_VAR_11

testKey12:
  value: testValue-12
  env: TEST_ENV_VAR_12
`
