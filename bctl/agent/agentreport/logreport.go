package agentreport

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bastionzero.com/bctl/v1/bctl/agent/controlchannel/agentidentity"
	"bastionzero.com/bctl/v1/bzerolib/connection/httpclient"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	uploadLogsEndpoint = "/api/v2/upload-logs/agent"
	bzeroLogFilePath   = "/var/log/bzero/bzero-agent.log"
	dateFormatFull     = "2006-01-02"
)

func ReportLogs(targetType string,
	agentIdentityProvider agentidentity.IAgentIdentityProvider,
	ctx context.Context,
	serviceUrl string,
	userEmail string,
	uploadLogsRequestId string) error {

	var archiveToPost *bytes.Buffer
	if targetType == "cluster" {
		if kubeLogArchive, err := createKubeLogArchive(ctx); err != nil {
			return err
		} else {
			archiveToPost = kubeLogArchive
		}
	} else {
		// create a temporary zip file with current log file and at most, last 2 rotated log files
		if bzeroLogArchive, err := createBzeroLogArchive(); err != nil {
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

	agentIdentityToken, tokenErr := agentIdentityProvider.GetToken(ctx)
	if tokenErr != nil {
		return fmt.Errorf("error getting agent identity token: %s", tokenErr)
	}

	headers := http.Header{
		"Content-Type":  {contentType},
		"Authorization": {fmt.Sprintf("Bearer %s", agentIdentityToken)},
	}
	options := httpclient.HTTPOptions{
		Endpoint: uploadLogsEndpoint,
		Body:     buffer,
		Headers:  headers,
	}

	if client, err := httpclient.New(serviceUrl, options); err != nil {
		return fmt.Errorf("error creating new httpclient: %s", err)
	} else {
		client.Post(ctx)
	}

	return nil
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
		return nil, fmt.Errorf("error in getting access to K8S: %s", err)
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

func createBzeroLogArchive() (*bytes.Buffer, error) {
	filesFilteredByDate, err := retrieveFilePaths()
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
		fullpath := filepath.Join(filepath.Dir(bzeroLogFilePath), file)
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

func retrieveFilePaths() ([]string, error) {
	directory, err := os.Open(filepath.Dir(bzeroLogFilePath))
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
	filesFilteredByDate = append(filesFilteredByDate, filepath.Base(bzeroLogFilePath))
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
		return nil, "", fmt.Errorf("failed creating form field UserEmail: %s", err)
	} else {
		userEmailWriter.Write([]byte(userEmail))
	}

	if requestIdWriter, err := formDataWriter.CreateFormField("UploadLogsRequestId"); err != nil {
		return nil, "", fmt.Errorf("failed creating form field UploadLogsRequestId: %s", err)
	} else {
		requestIdWriter.Write([]byte(uploadLogsRequestId))
	}

	if formFileWriter, err := formDataWriter.CreateFormFile("LogArchiveZip", "archive.zip"); err != nil {
		return nil, "", fmt.Errorf("failed creating form file LogArchiveZip: %s", err)
	} else {
		io.Copy(formFileWriter, zipArchive)
	}

	contentType := formDataWriter.FormDataContentType()

	return formDataBuffer, contentType, nil
}
