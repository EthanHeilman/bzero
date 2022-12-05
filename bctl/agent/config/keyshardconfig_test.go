package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/config/client"
	"bastionzero.com/bctl/v1/bctl/agent/config/data"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func initializeConfigFile(path string, contents string) {
	file, _ := os.Create(path)
	file.WriteString(contents)
}

func expectFileKeyUnset(path string, key data.SplitPrivateKey) {
	rawData, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var actual data.KeyShardData
	err = json.Unmarshal(rawData, &actual)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse JSON: %s -- raw data: '%s'", err, rawData))

	_, err = findEntry(actual, key)
	Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("expected entry to be unset but got err: %s", err))
}

func expectFileKeySetTo(path string, key data.SplitPrivateKey, expectedEntry data.KeyEntry) {
	rawData, err := os.ReadFile(path)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to read config file %s: %s", path, err))

	var actual data.KeyShardData
	err = json.Unmarshal(rawData, &actual)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to parse JSON: %s -- raw data: '%s'", err, rawData))

	idx, err := findEntry(actual, key)
	Expect(err).To(BeNil(), fmt.Sprintf("failed to find entry: %s", err))

	expectEntryToEqual(actual[idx], expectedEntry)
}

func expectEntryToEqual(actual data.KeyEntry, expected data.KeyEntry) {
	Expect(expected.Key).To(Equal(actual.Key), "Keys do not match:\nActual: %+v\nExpected: %+v", actual.Key, expected.Key)
	Expect(expected.TargetIds).To(ContainElements(actual.TargetIds), "TargetIds do not match:\nActual: %+v\nExpected: %+v", actual.TargetIds, expected.TargetIds)
}

// note that this suite employs two methods of testing the config object's behavior. The first is by mocking the underlying client and
// checking that load and save operations occur with the correct data. This is simpler and sufficient for many tests.
// However, for concurrency tests we cannot make guarantees about what data might be loaded and saved in what order, so for these we actually
// create a temporary config file with a real systemd client. We populate and check its contents directly.
var _ = Describe("Key Shard Config", Ordered, func() {
	tmpConfigFile := "keyshards.json"

	var checkPath, tempDir string

	Context("Adding entries", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)
				mockClient.On("Save", data.DefaultMockKeyShardDataSmall()).Return(nil)

				By("adding an entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(data.DefaultMockKeyShardDataSmall()[0])
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add key shard: %s", err))
			})

			It("Adds the provided entry to config", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Entry does not exist / huge key", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			hugeKey := data.KeyEntry{
				Key: data.SplitPrivateKey{
					E: []byte{79, 116, 37, 139, 191, 91, 226, 179, 1, 106, 21, 202, 49, 114, 147, 86, 6, 77, 201, 142, 152, 163, 90, 69, 69, 0, 144, 50, 145, 231, 126, 212, 124, 189, 72, 3, 102, 11, 132, 183, 94, 131, 104, 134, 250, 121, 116, 43, 63, 149, 188, 37, 216, 220, 78, 218, 55, 232, 138, 236, 138, 214, 29, 197, 228, 0, 252, 194, 129, 179, 46, 164, 154, 226, 38, 174, 164, 177, 179, 99, 66, 118, 167, 92, 40, 179, 167, 8, 192, 101, 226, 222, 231, 149, 180, 158, 153, 76, 211, 187, 118, 72, 253, 103, 98, 204, 90, 144, 170, 124, 29, 98, 248, 159, 199, 94, 239, 11, 17, 194, 194, 88, 77, 159, 2, 59, 201, 175, 172, 92, 178, 70, 45, 188, 21, 229, 161, 232, 143, 137, 208, 210, 182, 120, 125, 164, 142, 7, 181, 1, 49, 212, 121, 223, 37, 242, 254, 190, 252, 234, 45, 29, 61, 43, 229, 21, 79, 181, 140, 220, 239, 71, 48, 225, 26, 67, 137, 200, 199, 51, 42, 37, 20, 167, 83, 155, 170, 246, 232, 171, 117, 189, 138, 131, 132, 23, 128, 140, 139, 242, 241, 98, 159, 134, 237, 131, 229, 103, 131, 237, 146, 122, 25, 38, 0, 222, 28, 129, 61, 81, 36, 205, 5, 17, 64, 220, 13, 14, 180, 196, 202, 89, 110, 22, 190, 210, 126, 8, 202, 166, 231, 106, 205, 173, 250, 204, 255, 2, 15, 30, 57, 2, 203, 235, 149, 135, 248, 174, 18, 200, 84, 214, 117, 149, 232, 80, 14, 119, 64, 226, 135, 1, 230, 217, 157, 99, 11, 246, 153, 214, 119, 128, 27, 126, 37, 81, 189, 51, 114, 110, 214, 188, 38, 69, 223, 8, 119, 165, 9, 131, 103, 162, 176, 61, 60, 205, 65, 82, 13, 178, 2, 168, 193, 218, 101, 83, 195, 30, 146, 124, 218, 241, 224, 165, 14, 159, 126, 98, 162, 33, 171, 115, 216, 125, 228, 106, 3, 70, 42, 24, 33, 114, 207, 93, 120, 53, 192, 44, 78, 103, 237, 151, 63, 11, 189, 47, 30, 131, 171, 54, 210, 75, 136, 92, 149, 73, 52, 122, 216, 156, 187, 29, 184, 188, 173, 62, 6, 60, 14, 110, 116, 153, 101, 90, 135, 151, 191, 71, 6, 198, 220, 177, 163, 111, 35, 247, 42, 34, 90, 153, 118, 154, 147, 52, 19, 12, 214, 198, 137, 129, 125, 43, 159, 188, 238, 173, 139, 250, 9, 140, 73, 215, 72, 24, 7, 56, 82, 3, 165, 124, 236, 95, 156, 219, 204, 210, 146, 213, 247, 244, 158, 196, 87, 73, 178, 132, 76, 38, 234, 186, 224, 39, 240, 123, 220, 22, 100, 130, 53, 207, 127, 95, 199, 158, 83, 27, 153, 242, 187, 61, 201, 211, 227, 76, 211, 71, 213, 125, 235, 12, 142, 31, 21, 53, 16, 230, 105, 154, 92, 167, 108, 180, 165, 70, 171, 225, 114, 244, 250, 74, 201, 115, 113, 216, 145, 195, 130, 168, 160, 191, 174, 19},
					D: []byte{233, 163, 65, 106, 216, 196, 145, 29, 225, 92, 47, 137, 164, 251, 82, 34, 125, 124, 215, 6, 17, 236, 242, 144, 241, 40, 83, 210, 31, 148, 112, 46, 0, 116, 146, 211, 233, 210, 32, 200, 15, 148, 130, 65, 81, 54, 94, 7, 68, 30, 92, 122, 153, 43, 68, 94, 74, 41, 129, 6, 212, 57, 17, 206, 59, 52, 184, 164, 85, 59, 45, 150, 13, 5, 155, 4, 69, 32, 177, 58, 99, 200, 239, 250, 93, 188, 51, 197, 181, 113, 30, 199, 198, 8, 165, 246, 206, 208, 131, 182, 161, 216, 56, 63, 247, 80, 209, 197, 117, 184, 19, 221, 65, 49, 127, 88, 29, 1, 157, 30, 225, 67, 40, 197, 182, 185, 110, 75, 41, 205, 227, 70, 197, 75, 32, 191, 173, 94, 44, 178, 68, 31, 126, 246, 154, 203, 1, 21, 82, 198, 67, 184, 176, 151, 155, 213, 23, 41, 196, 221, 44, 11, 32, 201, 124, 103, 100, 143, 74, 88, 142, 233, 95, 211, 105, 195, 251, 88, 65, 46, 197, 232, 162, 240, 237, 159, 204, 246, 39, 53, 222, 125, 68, 187, 159, 27, 144, 187, 178, 103, 81, 87, 219, 8, 148, 212, 31, 205, 67, 65, 237, 4, 135, 34, 30, 139, 252, 128, 239, 246, 104, 93, 117, 122, 234, 64, 84, 109, 195, 237, 16, 106, 200, 208, 74, 212, 190, 67, 251, 199, 89, 190, 232, 31, 18, 237, 206, 249, 40, 87, 164, 210, 87, 233, 251, 189, 81, 97, 240, 88, 234, 86, 169, 87, 168, 188, 157, 17, 176, 6, 193, 17, 101, 101, 29, 144, 35, 39, 49, 81, 148, 96, 146, 196, 38, 165, 199, 239, 80, 97, 45, 61, 224, 23, 1, 237, 139, 101, 82, 20, 153, 203, 176, 175, 137, 104, 84, 124, 210, 2, 103, 159, 236, 229, 128, 118, 233, 97, 39, 31, 73, 209, 155, 105, 152, 198, 253, 114, 192, 41, 102, 176, 25, 165, 212, 165, 125, 72, 57, 75, 6, 40, 53, 242, 159, 34, 194, 7, 214, 207, 7, 188, 186, 10, 18, 92, 80, 35, 212, 89, 10, 24, 35, 79, 80, 172, 116, 77, 251, 231, 15, 118, 217, 83, 105, 169, 166, 63, 220, 220, 54, 159, 121, 221, 100, 54, 244, 138, 101, 143, 235, 201, 207, 93, 119, 68, 63, 228, 247, 214, 28, 221, 15, 192, 175, 75, 56, 249, 49, 14, 143, 44, 231, 213, 126, 28, 42, 165, 104, 127, 75, 133, 3, 15, 91, 37, 86, 60, 171, 222, 206, 40, 110, 197, 228, 245, 97, 50, 230, 172, 36, 61, 58, 242, 103, 182, 31, 73, 80, 160, 147, 64, 0, 139, 73, 186, 81, 219, 158, 133, 207, 108, 159, 196, 64, 172, 254, 175, 249, 185, 155, 71, 19, 45, 133, 8, 175, 142, 54, 117, 86, 183, 17, 254, 179, 121, 253, 115, 152, 171, 157, 240, 153, 161, 150, 205, 31, 217, 14, 23, 103, 232, 150, 22, 116, 221, 83, 91, 177, 56, 128, 90, 46, 180, 93, 214, 146, 177, 117, 20, 50, 242, 17, 154, 65, 42, 204, 210, 31, 113, 226, 91, 22, 179, 254, 231, 154, 173, 80, 33, 103, 249, 6, 27, 246, 208, 76, 245, 144, 235, 32, 187, 190, 94, 106, 207, 250, 60, 147, 100, 226, 111, 74, 97, 199, 56, 53, 59, 111, 92, 233, 30, 161, 10, 157, 176, 120, 95, 160, 220, 133, 64, 175, 236, 143, 11, 247, 207, 108, 248, 47, 90, 188, 94, 200, 191, 112, 15, 241, 239, 217, 144, 194, 47, 218, 223, 1, 133, 247, 202, 69, 139, 93, 61, 140, 152, 141, 236, 216, 237, 132, 170, 120, 156, 252, 88, 202, 64, 3, 76, 244, 248, 70, 163, 229, 69, 136, 199, 143, 155, 30, 130, 255, 27, 101, 212, 166, 124, 250, 242, 161, 108, 37, 135, 214, 45, 217, 206, 76, 114, 181, 28, 167, 225, 182, 17, 11, 91, 140, 157, 117, 214, 151, 124, 245, 186, 253, 245, 179, 221, 74, 189, 54, 134, 187, 201, 52, 191, 87, 65, 134, 90, 86, 226, 176, 126, 57, 88, 132, 205, 111, 242, 235, 229, 180, 185, 119, 140, 45, 193, 111, 164, 13, 157, 254, 49, 21, 236, 191, 173, 64, 149, 37, 125, 79, 132, 128, 11, 168, 27, 127, 210, 150, 90, 5, 211, 154, 125, 205, 19, 175, 81, 239, 253, 248, 232, 209, 177, 190, 191, 141, 210, 98, 97, 179, 92, 56, 153, 197, 161, 2, 112, 141, 160, 238, 103, 85, 168, 57, 239, 130, 123, 217, 174, 109, 170, 201, 205, 192, 224, 148, 42, 179, 199, 202, 250, 88, 208, 220, 81, 80, 4, 98, 109, 181, 63, 17, 22, 137, 127, 6, 205, 253, 13, 45, 101, 243, 167, 77, 123, 168, 205, 182, 2, 118, 178, 35, 158, 196, 35, 84, 133, 159, 5, 143, 165, 152, 231, 108, 55, 202, 58, 100, 157, 194, 137, 58, 112, 131, 4, 162, 229, 131, 18, 70, 196, 118, 212, 72, 99, 6, 128, 129, 41, 3, 5, 185, 221, 96, 88, 104, 204, 183, 156, 172, 115, 241, 210, 199, 11, 66, 87, 225, 67, 205, 14, 132, 6, 169, 181, 85, 255, 219, 159, 211, 56, 13, 110, 104, 207, 195, 38, 111, 134, 149, 112, 83, 118, 55, 164, 228, 77, 170, 139, 236, 65, 205, 203, 192, 87, 35, 182, 81, 204, 126, 172, 199, 219, 127, 165, 190, 95, 155, 81, 172, 47, 237, 210, 50, 250, 139, 127, 223, 196, 97, 65, 110, 1, 188, 227, 122, 150, 227, 198, 169, 194, 239, 249, 16, 139, 93, 188, 69, 208, 166, 236, 229, 9, 199, 65, 5, 203, 156, 175, 213, 225, 23, 0, 142, 255, 151, 105, 41, 120, 131, 233, 170, 250, 82, 174, 136, 178, 127, 80, 227, 117, 237, 77, 114, 227, 88, 242, 170, 59, 227, 231, 66, 149, 84, 106, 100, 232, 129, 4, 8, 45, 49, 213, 133, 60, 179, 127, 31, 46, 187, 193, 63, 223, 181, 61, 111, 79, 35, 223, 99, 73, 111, 27, 19, 180, 168, 14, 190, 115, 23, 123, 205, 3, 36, 119, 74, 217, 37, 153, 117, 18, 71, 183, 247, 20, 224, 3, 72, 37, 93, 250, 105, 124, 77, 138, 170, 0, 42, 178, 189, 114, 19, 160, 45, 43, 193, 190, 54, 138, 238, 223, 190, 37, 94, 140, 204, 79, 192, 225, 82, 52, 231, 81, 123, 146, 189, 127, 63, 104, 117, 20, 140, 23, 0, 18, 166, 105, 62, 96, 122, 36, 54, 247, 226, 43, 131, 13, 198, 115, 75, 221, 87, 234, 246, 68, 118, 224, 180, 212, 23, 203, 46, 202, 52, 157, 44, 74, 50, 36, 173, 175, 187, 74, 32, 243, 245, 133, 108, 11, 31, 215, 54, 165, 204, 29, 17, 251, 126, 206, 119, 136, 187, 170, 101, 124, 194, 101, 66, 214, 241, 219, 83, 64, 176, 123, 83, 24, 83, 44, 164, 77, 36, 127, 68, 206, 69, 234, 238, 180, 155, 121, 18, 82, 36, 238, 218, 194, 155, 163, 173, 245, 0, 222, 119, 81, 204, 163, 8, 238, 80, 151, 244, 150, 177, 174, 176, 168, 22, 193, 141, 243, 157, 174, 217, 42, 114, 230, 225, 231, 165, 0, 156, 221, 114, 85, 213, 208, 215, 241, 217, 143, 100, 8, 76, 69, 98, 230, 114, 136, 59, 107, 161, 18, 53, 177, 94, 146, 95, 83, 215, 207, 136, 18, 142, 17, 60, 14, 225, 17, 104, 94, 149, 71, 161, 112, 73, 132, 112, 251, 140, 57, 140, 249, 98, 27, 121, 129, 70, 233, 22, 88, 53, 67, 244, 254, 165, 2, 176, 210, 139, 162, 233, 4, 54, 94, 189, 19, 160, 95, 211, 239, 12, 195, 100, 197, 224, 78, 40, 15, 122, 128, 117, 117, 34, 228, 55, 114, 183, 89, 122, 31, 174, 255, 81, 245, 234, 25, 58, 115, 28, 173, 209, 39, 42, 213, 160, 6, 135, 19, 9, 206, 74, 32, 61, 36, 64, 133, 36, 148, 218, 151, 86, 130, 42, 99, 140, 220, 150, 189, 237, 23, 222, 249, 15, 156, 89, 89, 84, 250, 61, 209, 238, 13, 87, 94, 178, 103, 95, 94, 170, 58, 204, 58, 195, 177, 72, 31, 228, 8, 0, 227, 74, 107, 50, 196, 160, 239, 187, 102, 211, 74, 158, 18, 196, 157, 192, 147, 2, 184, 203, 212, 164, 43, 162, 228, 191, 160, 220, 63, 129, 32, 22, 152, 3, 41, 62, 73, 115, 194, 7, 137, 201, 207, 24, 139, 74, 47, 112, 123, 102, 150, 104, 7, 65, 245, 211, 206, 103, 148, 98, 52, 78, 34, 153, 235, 113, 73, 86, 148, 41, 254, 239, 195, 65, 144, 114, 104, 197, 27, 162, 0, 248, 11, 124, 103, 186, 58, 159, 150, 177, 31, 59, 87, 135, 72, 28, 109, 251, 126, 113, 97, 139, 88, 139, 25, 24, 16, 152, 250, 176, 9, 196, 102, 250, 52, 101, 16, 243, 107, 204, 111, 169, 29, 32, 73, 33, 136, 104, 93, 121, 117, 112, 252, 0, 233, 31, 102, 33, 217, 105},
					PublicKey: data.PublicKey{
						E: 65537,
						N: []byte{79, 116, 37, 139, 191, 91, 226, 179, 1, 106, 21, 202, 49, 114, 147, 86, 6, 77, 201, 142, 152, 163, 90, 69, 69, 0, 144, 50, 145, 231, 126, 212, 124, 189, 72, 3, 102, 11, 132, 183, 94, 131, 104, 134, 250, 121, 116, 43, 63, 149, 188, 37, 216, 220, 78, 218, 55, 232, 138, 236, 138, 214, 29, 197, 228, 0, 252, 194, 129, 179, 46, 164, 154, 226, 38, 174, 164, 177, 179, 99, 66, 118, 167, 92, 40, 179, 167, 8, 192, 101, 226, 222, 231, 149, 180, 158, 153, 76, 211, 187, 118, 72, 253, 103, 98, 204, 90, 144, 170, 124, 29, 98, 248, 159, 199, 94, 239, 11, 17, 194, 194, 88, 77, 159, 2, 59, 201, 175, 172, 92, 178, 70, 45, 188, 21, 229, 161, 232, 143, 137, 208, 210, 182, 120, 125, 164, 142, 7, 181, 1, 49, 212, 121, 223, 37, 242, 254, 190, 252, 234, 45, 29, 61, 43, 229, 21, 79, 181, 140, 220, 239, 71, 48, 225, 26, 67, 137, 200, 199, 51, 42, 37, 20, 167, 83, 155, 170, 246, 232, 171, 117, 189, 138, 131, 132, 23, 128, 140, 139, 242, 241, 98, 159, 134, 237, 131, 229, 103, 131, 237, 146, 122, 25, 38, 0, 222, 28, 129, 61, 81, 36, 205, 5, 17, 64, 220, 13, 14, 180, 196, 202, 89, 110, 22, 190, 210, 126, 8, 202, 166, 231, 106, 205, 173, 250, 204, 255, 2, 15, 30, 57, 2, 203, 235, 149, 135, 248, 174, 18, 200, 84, 214, 117, 149, 232, 80, 14, 119, 64, 226, 135, 1, 230, 217, 157, 99, 11, 246, 153, 214, 119, 128, 27, 126, 37, 81, 189, 51, 114, 110, 214, 188, 38, 69, 223, 8, 119, 165, 9, 131, 103, 162, 176, 61, 60, 205, 65, 82, 13, 178, 2, 168, 193, 218, 101, 83, 195, 30, 146, 124, 218, 241, 224, 165, 14, 159, 126, 98, 162, 33, 171, 115, 216, 125, 228, 106, 3, 70, 42, 24, 33, 114, 207, 93, 120, 53, 192, 44, 78, 103, 237, 151, 63, 11, 189, 47, 30, 131, 171, 54, 210, 75, 136, 92, 149, 73, 52, 122, 216, 156, 187, 29, 184, 188, 173, 62, 6, 60, 14, 110, 116, 153, 101, 90, 135, 151, 191, 71, 6, 198, 220, 177, 163, 111, 35, 247, 42, 34, 90, 153, 118, 154, 147, 52, 19, 12, 214, 198, 137, 129, 125, 43, 159, 188, 238, 173, 139, 250, 9, 140, 73, 215, 72, 24, 7, 56, 82, 3, 165, 124, 236, 95, 156, 219, 204, 210, 146, 213, 247, 244, 158, 196, 87, 73, 178, 132, 76, 38, 234, 186, 224, 39, 240, 123, 220, 22, 100, 130, 53, 207, 127, 95, 199, 158, 83, 27, 153, 242, 187, 61, 201, 211, 227, 76, 211, 71, 213, 125, 235, 12, 142, 31, 21, 53, 16, 230, 105, 154, 92, 167, 108, 180, 165, 70, 171, 225, 114, 244, 250, 74, 201, 115, 113, 216, 145, 195, 130, 168, 160, 191, 174, 19},
					},
				},
			}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)
				mockClient.On("Save", data.KeyShardData{hugeKey}).Return(nil)

				By("adding a production-size entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(hugeKey)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add key shard: %s", err))
			})

			It("Adds the provided entry to config", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("entry exists / new target", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.DefaultMockKeyShardDataSmall()
				newData[0].TargetIds = append(newData[0].TargetIds, "targetId3")
				mockClient.On("Save", newData).Return(nil)

				By("adding an entry with a matching key but a new target")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(data.DefaultMockKeyEntry3Target())
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add entry: %s", err))
			})

			It("Adds the new targets to the existing entry", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("entry exists / no new targets", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				currentData := data.DefaultMockKeyShardDataSmall()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				By("adding an entry that matches exactly")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddKey(currentData[0])
			})

			It("Returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many entries at once / different keys", Ordered, func() {
			BeforeAll(func() {
				By("starting with an empty config")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding 12 distinct entries")
				for i := 1; i <= 12; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.D = []byte(fmt.Sprintf("%d", i))
					go config.AddKey(data.KeyEntry{
						Key:       newKey,
						TargetIds: data.DefaultMockTargetIds(),
					})
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("Sets all values in the file", func() {
				By("checking that each entry has the data we wrote")
				for i := 1; i <= 12; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.D = []byte(fmt.Sprintf("%d", i))
					expectFileKeySetTo(checkPath, newKey, data.KeyEntry{
						Key:       newKey,
						TargetIds: data.DefaultMockTargetIds(),
					})
				}
			})
		})

		When("Adding many entries at once / same key", Ordered, func() {
			BeforeAll(func() {
				By("starting with an empty config")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding 12 targets to the same entry")
				for i := 1; i <= 12; i++ {
					go config.AddKey(data.KeyEntry{
						Key:       data.DefaultMockSplitPrivateKey(),
						TargetIds: []string{fmt.Sprintf("targetId%d", i)},
					})
				}

				// let the sets happeen
				time.Sleep(1 * time.Second)
			})

			It("Sets all values in the file", func() {
				expectedTargetIds := []string{}
				for i := 1; i <= 12; i++ {
					expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", i))
				}

				By("checking that each entry has the data we wrote")
				expectFileKeySetTo(checkPath, data.DefaultMockSplitPrivateKey(), data.KeyEntry{
					Key:       data.DefaultMockSplitPrivateKey(),
					TargetIds: expectedTargetIds,
				})
			})
		})
	})

	Context("Adding targets", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddTarget(data.SplitPrivateKey{}, "targetId")
			})

			It("Returns a KeyError without saving", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry exists / new target", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)
				mockClient.On("Save", data.KeyShardData{data.DefaultMockKeyEntry3Target()}).Return(nil)

				By("adding a new target to that entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddTarget(data.DefaultMockSplitPrivateKey(), "targetId3")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to add target: %s", err))
			})

			It("adds the target to the entry", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Entry exists / target exists", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				By("adding an existing target to that entry")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.AddTarget(data.DefaultMockSplitPrivateKey(), "targetId2")
			})

			It("Returns a NoOpError without saving", func() {
				Expect(errors.Is(err, &NoOpError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Adding many targets at once / different entries", Ordered, func() {
			BeforeAll(func() {
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				By("starting with a config with many entries")
				initializeConfigFile(checkPath, data.MockKeyShardLargeNoTargetsRaw())

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding a few targets to each entry")
				for i := 1; i <= 4; i++ {
					for j := 1; j <= 12; j++ {
						if j%4 == i {
							go config.AddTarget(data.SplitPrivateKey{D: []byte(fmt.Sprintf("%d", i))}, fmt.Sprintf("targetId%d", i))
						}
					}
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("adds the targets", func() {
				for i := 1; i <= 4; i++ {
					expectedTargetIds := []string{}
					for j := 1; j <= 12; j++ {
						if j%4 == i {
							expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", j))
						}
					}

					By("checking that each entry has the data we wrote")
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.D = []byte(fmt.Sprintf("%d", i))
					expectFileKeySetTo(checkPath, newKey, data.KeyEntry{
						Key:       newKey,
						TargetIds: expectedTargetIds,
					})
				}
			})
		})

		When("Adding many targets at once / same entry, some existing", Ordered, func() {
			BeforeAll(func() {
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				By("starting with a config with one entry")
				mockData := data.DefaultMockKeyShardDataSmall()
				dataBytes, _ := json.Marshal(mockData)
				initializeConfigFile(checkPath, string(dataBytes))

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("adding a mix of existing and new targetIds to the entry")
				for i := 1; i <= 12; i++ {
					go config.AddTarget(data.DefaultMockSplitPrivateKey(), fmt.Sprintf("targetId%d", i))
				}

				// let the adds happeen
				time.Sleep(1 * time.Second)
			})

			It("adds the targets", func() {
				expectedTargetIds := []string{}
				for i := 1; i <= 12; i++ {
					expectedTargetIds = append(expectedTargetIds, fmt.Sprintf("targetId%d", i))
				}

				By("checking that all targets are present in the entry")
				expectFileKeySetTo(checkPath, data.DefaultMockSplitPrivateKey(), data.KeyEntry{
					Key:       data.DefaultMockSplitPrivateKey(),
					TargetIds: expectedTargetIds,
				})
			})
		})
	})

	Context("LastKey", func() {
		When("Target does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				_, err = config.LastKey("targetId1000")
			})

			It("Returns a TargetError without saving", func() {
				Expect(errors.Is(err, &TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists in multiple entries", Ordered, func() {
			var err error
			var config *KeyShardConfig
			var key data.SplitPrivateKey
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				mockClient.On("FetchKeyShardData").Return(data.MockKeyShardDataMedium(), nil)

				By("requesting a target present in both")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				key, err = config.LastKey("targetId1")
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get key: %s", err))
			})

			It("returns the most recent key", func() {
				Expect(key.D).To(Equal([]byte("101")))
			})
		})

		When("Target only exists in earliest entry", Ordered, func() {
			var err error
			var config *KeyShardConfig
			var key data.SplitPrivateKey
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				specialTarget := "targetId-special"
				mockData := data.MockKeyShardDataMedium()
				mockData[0].TargetIds = append(mockData[0].TargetIds, specialTarget)
				mockClient.On("FetchKeyShardData").Return(mockData, nil)

				By("requesting a target only present in the earlier one")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				key, err = config.LastKey(specialTarget)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to get key: %s", err))
			})

			It("returns the earlier key", func() {
				Expect(key.D).To(Equal([]byte("123")))
			})
		})
	})

	Context("Deleting entries", func() {
		When("Entry does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with an empty config")
				mockClient.On("FetchKeyShardData").Return(data.KeyShardData{}, nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteKey(data.SplitPrivateKey{})
			})

			It("Returns a KeyError without saving", func() {
				Expect(errors.Is(err, &KeyError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Entry exists", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				mockClient.On("FetchKeyShardData").Return(data.MockKeyShardDataMedium(), nil)
				mockClient.On("Save", data.AltMockKeyShardDataSmall()).Return(nil)

				By("deleting one of them")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteKey(data.DefaultMockSplitPrivateKey())
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete entry: %s", err))
			})

			It("deletes the entry", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Deleting many entries at once", Ordered, func() {
			keyOne := data.AltMockKeyShardDataSmall()[0].Key
			keyOne.D = []byte("1")

			BeforeAll(func() {
				By("starting with a config with many entries")
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)
				initializeConfigFile(checkPath, data.MockKeyShardLargeWithTargetsRaw())

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())
				By("deleting all but one entry")
				for i := 1; i <= 3; i++ {
					go config.DeleteKey(data.SplitPrivateKey{D: []byte(fmt.Sprintf("%d", i))})
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			It("deletes the entries", func() {
				By("checking that the deleted entries are absent")
				expectFileKeyUnset(checkPath, keyOne)
				for i := 2; i <= 3; i++ {
					newKey := data.DefaultMockSplitPrivateKey()
					newKey.D = []byte(fmt.Sprintf("%d", i))
					expectFileKeyUnset(checkPath, newKey)
				}
			})

			It("leaves the un-deleted entries", func() {
				By("checking that the un-deleted entry has not been modified")
				newKey := data.DefaultMockSplitPrivateKey()
				newKey.D = []byte("4")
				expectFileKeySetTo(checkPath, newKey, data.KeyEntry{
					Key:       newKey,
					TargetIds: []string{"targetId6", "targetId7"},
				})
			})
		})
	})

	Context("Deleting targets", func() {
		When("Target does not exist", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with one entry")
				mockClient.On("FetchKeyShardData").Return(data.DefaultMockKeyShardDataSmall(), nil)

				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteTarget("target", false)
			})

			It("Returns a TargetError", func() {
				Expect(errors.Is(err, &TargetError{}), fmt.Sprintf("got wrong error type: %s", err))
			})
		})

		When("Target exists / soft delete", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.MockKeyShardDataMedium()
				newData[1].TargetIds = []string{"targetId1"}
				mockClient.On("Save", newData).Return(nil)

				By("deleting the most recent instance of one target")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteTarget("targetId2", false)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete target: %s", err))
			})

			It("removes the target from the most recent entry without affecting the other entries", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Target exists / hard delete", Ordered, func() {
			var err error
			var config *KeyShardConfig
			mockClient := &MockClient{}

			BeforeAll(func() {
				By("starting with a config with two entries")
				currentData := data.MockKeyShardDataMedium()
				mockClient.On("FetchKeyShardData").Return(currentData, nil)

				newData := data.MockKeyShardDataMedium()
				newData[0].TargetIds = []string{"targetId1"}
				newData[1].TargetIds = []string{"targetId1"}
				mockClient.On("Save", newData).Return(nil)

				By("deleting a target present in both entries")
				config, err = LoadKeyShardConfig(mockClient)
				Expect(err).To(BeNil())
				err = config.DeleteTarget("targetId2", true)
			})

			It("returns a nil error", func() {
				Expect(err).To(BeNil(), fmt.Sprintf("failed to delete target: %s", err))
			})

			It("removes the target from all entries", func() {
				By("ensuring that we saved the correct data to the underlying client")
				mockClient.AssertExpectations(GinkgoT())
			})
		})

		When("Deleting many targets at once", Ordered, func() {
			BeforeAll(func() {
				tempDir = GinkgoT().TempDir()
				checkPath = filepath.Join(tempDir, tmpConfigFile)

				By("starting with a config with many entries")
				initializeConfigFile(checkPath, data.MockKeyShardLargeWithTargetsRaw())

				client, _ := client.NewSystemdClient(tempDir, client.KeyShard)
				config, err := LoadKeyShardConfig(client)
				Expect(err).To(BeNil())

				By("deleting all but one of the targets")
				for i := 1; i <= 8; i++ {
					go config.DeleteTarget(fmt.Sprintf("targetId%d", i), false)
				}

				// let the deletes happeen
				time.Sleep(1 * time.Second)
			})

			It("deletes all the correct targets", func() {
				By("checking that all of the deleted targets are absent from their original entry")
				newKey := data.DefaultMockSplitPrivateKey()
				newKey.D = []byte("2")
				expectFileKeySetTo(checkPath, newKey, data.KeyEntry{
					Key:       newKey,
					TargetIds: []string{},
				})

				newKey.D = []byte("3")
				expectFileKeySetTo(checkPath, newKey, data.KeyEntry{
					Key:       newKey,
					TargetIds: []string{},
				})

				newKey.D = []byte("4")
				expectFileKeySetTo(checkPath, newKey, data.KeyEntry{
					Key:       newKey,
					TargetIds: []string{},
				})
			})

			It("leaves the un-deleted target", func() {
				keyOne := data.AltMockSplitPrivateKey()
				keyOne.D = []byte("1")
				By("checking that the remaining target is still in its original entry")
				expectFileKeySetTo(checkPath, keyOne, data.KeyEntry{
					Key:       keyOne,
					TargetIds: []string{"targetId0"},
				})
			})
		})
	})
})
