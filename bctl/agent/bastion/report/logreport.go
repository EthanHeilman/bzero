package report

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bastionzero.com/agent/agenttype"
	"bastionzero.com/agent/bastion"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	uploadLogsEndpoint = "/api/v2/upload-logs/agent"
	dateFormatFull     = "2006-01-02"
)

func ReportLogs(
	ctx context.Context,
	bastion bastion.ApiClient,
	agentType agenttype.AgentType,
	userEmail string,
	uploadLogsRequestId string,
	logFilePath string,
) error {

	var archiveToPost *bytes.Buffer
	switch agentType {
	case agenttype.Kubernetes:
		if kubeLogArchive, err := createKubeLogArchive(ctx); err != nil {
			return err
		} else {
			archiveToPost = kubeLogArchive
		}
	case agenttype.Linux, agenttype.Windows:
		// create a temporary zip file with current log file and at most, last 2 rotated log files
		if bzeroLogArchive, err := createBzeroLogArchive(logFilePath); err != nil {
			return err
		} else {
			archiveToPost = bzeroLogArchive
		}
	}

	// create multipart form data to post to Bastion
	buffer, contentType, err := createFormData(userEmail, uploadLogsRequestId, archiveToPost)
	if err != nil {
		return err
	}

	// return fmt.Errorf("LOGS LENGTH: %d", buffer.Len())

	return bastion.ReportLogs(ctx, buffer, contentType)
}

func createKubeLogArchive(ctx context.Context) (*bytes.Buffer, error) {
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		return nil, fmt.Errorf("could not retrieve pod name")
	}
	podNameSpace := os.Getenv("NAMESPACE")
	if podNameSpace == "" {
		return nil, fmt.Errorf("could not retrieve pod namespace")
	}

	// consider pod logs in the last 48 hours
	// if pod started within that timeframe, return all pod logs
	var sinceSeconds int64 = 172800
	// also adding a limit on the amount of bytes returned
	var limitBytes int64 = 200 * 1024 * 1024
	podLogOpts := coreV1.PodLogOptions{
		SinceSeconds: &sinceSeconds,
		LimitBytes:   &limitBytes,
	}

	// get cluster config object for the pod
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error in getting config: %s", err)
	}
	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("error in getting access to Kubernetes: %s", err)
	}

	// create request to get logs from the pod
	req := clientset.CoreV1().Pods(podNameSpace).GetLogs(podName, &podLogOpts)
	// stream the logs into an io.ReadCloser
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("error in opening stream: %s", err)
	}
	defer podLogs.Close()

	// create a new zip archive
	zipArchive := new(bytes.Buffer)

	// create the zip writer
	zipWriter := zip.NewWriter(zipArchive)
	defer zipWriter.Close()

	// create a zip entry to write the kube pod logs into
	if zipEntryWriter, err := zipWriter.Create("kube-agent.log"); err != nil {
		return nil, fmt.Errorf("failed creating kube log zip file entry: %s", err)
	} else {
		io.Copy(zipEntryWriter, podLogs)
	}

	return zipArchive, nil
}

func createBzeroLogArchive(logFilePath string) (*bytes.Buffer, error) {
	filesFilteredByDate, err := retrieveFilePaths(logFilePath)
	if err != nil {
		return nil, err
	}

	// create a new zip archive
	zipArchive := new(bytes.Buffer)

	// create the zip writer
	zipWriter := zip.NewWriter(zipArchive)
	defer zipWriter.Close()

	// does this for 3 files max
	for _, file := range filesFilteredByDate {
		// build full file path for each log file
		fullpath := filepath.Join(filepath.Dir(logFilePath), file)
		logFile, err := os.Open(fullpath)
		if err != nil {
			return nil, fmt.Errorf("failed opening filtered bzero log file: %s", err)
		}
		defer logFile.Close()

		// create a zip entry for each file and copy each log file into it
		if zipEntryWriter, err := zipWriter.Create(file); err != nil {
			return nil, fmt.Errorf("failed creating bzero log zip file entry: %s", err)
		} else {
			io.Copy(zipEntryWriter, logFile)
		}
	}

	return zipArchive, nil
}

func retrieveFilePaths(logFilePath string) ([]string, error) {
	directory, err := os.Open(filepath.Dir(logFilePath))
	if err != nil {
		return nil, fmt.Errorf("failed opening bzero agent log directory: %s", err)
	}
	defer directory.Close()

	// retrieve all file names in bzero agent log directory
	filenames, err := directory.Readdirnames(0)
	if err != nil {
		return nil, fmt.Errorf("failed reading bzero log file names: %s", err)
	}

	// format of the timestamps in the names of rotated log files
	todayFull := time.Now().Format(dateFormatFull)
	yesterdayFull := time.Now().AddDate(0, 0, -1).Format(dateFormatFull)

	// filter the files by the date in timestamp
	var filesFilteredByDate []string
	filesFilteredByDate = append(filesFilteredByDate, filepath.Base(logFilePath))
	for _, filename := range filenames {
		if strings.Contains(filename, todayFull) || strings.Contains(filename, yesterdayFull) {
			filesFilteredByDate = append(filesFilteredByDate, filename)
		}
	}

	// sort the file names from most recent to oldest
	sort.Sort(sort.Reverse(sort.StringSlice(filesFilteredByDate)))

	// where to truncate the list of filenames
	truncateIndex := 3
	if len(filesFilteredByDate) < truncateIndex {
		truncateIndex = len(filesFilteredByDate)
	}

	// truncate the array after the 2nd rotated log file
	// so we retrieve only current log file and 2 rotated log files
	filesFilteredByDate = filesFilteredByDate[:truncateIndex]

	return filesFilteredByDate, nil
}

func createFormData(userEmail string, uploadLogsRequestId string, zipArchive *bytes.Buffer) (*bytes.Buffer, string, error) {
	formDataBuffer := new(bytes.Buffer)
	formDataWriter := multipart.NewWriter(formDataBuffer)
	defer formDataWriter.Close()

	if userEmailWriter, err := formDataWriter.CreateFormField("UserEmail"); err != nil {
		return nil, "", fmt.Errorf("failed creating form field UserEmail: %w", err)
	} else {
		userEmailWriter.Write([]byte(userEmail))
	}

	if requestIdWriter, err := formDataWriter.CreateFormField("UploadLogsRequestId"); err != nil {
		return nil, "", fmt.Errorf("failed creating form field UploadLogsRequestId: %w", err)
	} else {
		requestIdWriter.Write([]byte(uploadLogsRequestId))
	}

	if formFileWriter, err := formDataWriter.CreateFormFile("LogArchiveZip", "archive.zip"); err != nil {
		return nil, "", fmt.Errorf("failed creating form file LogArchiveZip: %w", err)
	} else {
		io.Copy(formFileWriter, zipArchive)
	}

	contentType := formDataWriter.FormDataContentType()

	return formDataBuffer, contentType, nil
}
