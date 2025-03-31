package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/melbahja/got"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

const (
	gcsLinkToken        = "gcsweb"
	gcsPrefix           = "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com"
	storagePrefix       = "https://storage.googleapis.com"
	artifactsPath       = "artifacts"
	promTarPath         = "audit-logs.tar"
	extraPath           = "gather-audit-logs"
	hypershiftExtraPath = "hypershift-dump-extra"
	e2ePrefix           = "e2e"
)

// ProwInfo stores all links and data collected via scanning for metrics
type ProwInfo struct {
	Started         time.Time
	Finished        time.Time
	AuditLogsTarURL string
}

// ProwJSON stores test start / finished timestamp
type ProwJSON struct {
	Timestamp int `json:"timestamp"`
}

// getLinksFromURL retrieves links from a given URL by parsing HTML content
func getLinksFromURL(netClient *http.Client, url string) ([]string, error) {
	links := []string{}

	resp, err := netClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %v", url, err)
	}
	defer resp.Body.Close()

	z := html.NewTokenizer(resp.Body)
	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			// End of the document, we're done
			return links, nil
		case tt == html.StartTagToken:
			t := z.Token()

			isAnchor := t.Data == "a"
			if isAnchor {
				for _, a := range t.Attr {
					if a.Key == "href" {
						links = append(links, a.Val)
						break
					}
				}
			}
		}
	}
}

// fetchAuditLogsTar retrieves metrics tarball URL from Prow information and constructs the expected metrics URL.
func fetchAuditLogsTar(logger *logrus.Logger, url *url.URL) (ProwInfo, error) {
	prowInfo, err := getTarURLFromProw(logger, url)
	if err != nil {
		return prowInfo, err
	}
	expectedAuditLogsTarURL := prowInfo.AuditLogsTarURL

	// Check that metrics/prometheus.tar can be fetched and it non-null
	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Head(expectedAuditLogsTarURL)
	if err != nil {
		return prowInfo, fmt.Errorf("failed to fetch %s: %v", expectedAuditLogsTarURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return prowInfo, fmt.Errorf("failed to check archive at %s: returned %s", expectedAuditLogsTarURL, resp.Status)
	}

	contentLength := resp.Header.Get("content-length")
	if contentLength == "" {
		return prowInfo, fmt.Errorf("failed to check archive at %s: no content length returned", expectedAuditLogsTarURL)
	}
	length, err := strconv.Atoi(contentLength)
	if err != nil {
		return prowInfo, fmt.Errorf("failed to check archive at %s: invalid content-length: %v", expectedAuditLogsTarURL, err)
	}
	if length == 0 {
		return prowInfo, fmt.Errorf("failed to check archive at %s: archive is empty", expectedAuditLogsTarURL)
	}
	return prowInfo, nil
}

func getTarURLFromProw(logger *logrus.Logger, baseURL *url.URL) (ProwInfo, error) {
	prowInfo := ProwInfo{}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	// Get a list of links on prow page
	prowToplinks, err := getLinksFromURL(netClient, baseURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("failed to find links at %s: %v", prowToplinks, err)
	}
	if len(prowToplinks) == 0 {
		return prowInfo, fmt.Errorf("no links found at %s", baseURL)
	}
	gcsTempURL := ""
	for _, link := range prowToplinks {
		logger.WithFields(logrus.Fields{"link": link})
		if strings.Contains(link, gcsLinkToken) {
			gcsTempURL = link
			break
		}
	}
	if gcsTempURL == "" {
		return prowInfo, fmt.Errorf("failed to find GCS link in %v", prowToplinks)
	}

	gcsURL, err := url.Parse(gcsTempURL)
	if err != nil {
		return prowInfo, fmt.Errorf("failed to parse GCS URL %s: %v", gcsTempURL, err)
	}

	// Fetch start and finish time of the test
	startTime, err := getTimeStampFromProwJSON(fmt.Sprintf("%s/started.json", gcsURL))
	if err != nil {
		return prowInfo, fmt.Errorf("failed to fetch test start time: %v", err)
	}
	prowInfo.Started = startTime

	finishedTime, err := getTimeStampFromProwJSON(fmt.Sprintf("%s/finished.json", gcsURL))
	if err != nil {
		return prowInfo, fmt.Errorf("failed to fetch test finshed time: %v", err)
	}
	prowInfo.Finished = finishedTime

	// Check that 'artifacts' folder is present
	gcsToplinks, err := getLinksFromURL(netClient, gcsURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("failed to fetch top-level GCS link at %s: %v", gcsURL, err)
	}
	if len(gcsToplinks) == 0 {
		return prowInfo, fmt.Errorf("no top-level GCS links at %s found", gcsURL)
	}
	tmpArtifactsURL := ""
	for _, link := range gcsToplinks {
		if strings.HasSuffix(link, "artifacts/") {
			tmpArtifactsURL = gcsPrefix + link
			break
		}
	}
	if tmpArtifactsURL == "" {
		return prowInfo, fmt.Errorf("failed to find artifacts link in %v", gcsToplinks)
	}
	artifactsURL, err := url.Parse(tmpArtifactsURL)
	if err != nil {
		return prowInfo, fmt.Errorf("failed to parse artifacts link %s: %v", tmpArtifactsURL, err)
	}

	// Get a list of folders in find ones which contain e2e
	artifactLinksToplinks, err := getLinksFromURL(netClient, artifactsURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("failed to fetch artifacts link at %s: %v", gcsURL, err)
	}
	if len(artifactLinksToplinks) == 0 {
		return prowInfo, fmt.Errorf("no artifact links at %s found", gcsURL)
	}
	tmpE2eURL := ""
	for _, link := range artifactLinksToplinks {
		logger.WithFields(logrus.Fields{"link": link})
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		logger.WithFields(logrus.Fields{"lastPathSegment": lastPathSegment})
		if strings.Contains(lastPathSegment, e2ePrefix) {
			tmpE2eURL = gcsPrefix + link
			break
		}
	}
	if tmpE2eURL == "" {
		return prowInfo, fmt.Errorf("failed to find e2e link in %v", artifactLinksToplinks)
	}
	e2eURL, err := url.Parse(tmpE2eURL)
	if err != nil {
		return prowInfo, fmt.Errorf("failed to parse e2e link %s: %v", tmpE2eURL, err)
	}

	// Support new-style jobs - look for gather-audit-logs
	var gatherExtraURL *url.URL

	e2eToplinks, err := getLinksFromURL(netClient, e2eURL.String())
	if err != nil {
		return prowInfo, fmt.Errorf("failed to fetch artifacts link at %s: %v", e2eURL, err)
	}
	if len(e2eToplinks) == 0 {
		return prowInfo, fmt.Errorf("no top links at %s found", e2eURL)
	}

	var candidates []*url.URL
	for _, link := range e2eToplinks {
		logger.WithFields(logrus.Fields{"link": link})
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		logger.WithFields(logrus.Fields{"lastPathSection": lastPathSegment})
		switch lastPathSegment {
		case "artifacts":
			continue
		case "gsutil":
			continue
		default:
			u, err := url.Parse(gcsPrefix + link)
			if err != nil {
				return prowInfo, fmt.Errorf("failed to parse e2e link %s: %v", tmpE2eURL, err)
			}
			candidates = append(candidates, u)
		}
	}

	switch len(candidates) {
	case 0:
		break
	case 1:
		gatherExtraURL = candidates[0]
	default:
		for _, u := range candidates {
			base := path.Base(u.Path)
			if base == extraPath || base == hypershiftExtraPath {
				gatherExtraURL = u
				break
			}
		}
	}

	if gatherExtraURL != nil {
		// New-style jobs may not have metrics available
		e2eToplinks, err = getLinksFromURL(netClient, gatherExtraURL.String())
		if err != nil {
			return prowInfo, fmt.Errorf("failed to fetch gather-extra link at %s: %v", e2eURL, err)
		}
		if len(e2eToplinks) == 0 {
			return prowInfo, fmt.Errorf("no top links at %s found", e2eURL)
		}
		for _, link := range e2eToplinks {
			logger.WithFields(logrus.Fields{"link": link})
			linkSplitBySlash := strings.Split(link, "/")
			lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
			if len(lastPathSegment) == 0 {
				lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
			}
			logger.WithFields(logrus.Fields{"lastPathSection": lastPathSegment})
			if lastPathSegment == artifactsPath {
				tmpGatherExtraURL := gcsPrefix + link
				gatherExtraURL, err = url.Parse(tmpGatherExtraURL)
				if err != nil {
					return prowInfo, fmt.Errorf("failed to parse e2e link %s: %v", tmpE2eURL, err)
				}
				break
			}
		}
		e2eURL = gatherExtraURL
	}

	tarFile := promTarPath

	gcsAuditLogURL := fmt.Sprintf("%s%s", e2eURL.String(), tarFile)
	tempAuditURL := strings.Replace(gcsAuditLogURL, gcsPrefix+"/gcs", storagePrefix, -1)
	expectedAuditLogURL, err := url.Parse(tempAuditURL)
	if err != nil {
		return prowInfo, fmt.Errorf("failed to parse metrics link %s: %v", tempAuditURL, err)
	}
	prowInfo.AuditLogsTarURL = expectedAuditLogURL.String()
	return prowInfo, nil
}

func getTimeStampFromProwJSON(rawURL string) (time.Time, error) {
	jsonURL, err := url.Parse(rawURL)
	if err != nil {
		return time.Now(), fmt.Errorf("failed to fetch prow JSOM at %s: %v", rawURL, err)
	}

	var netClient = &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Get(jsonURL.String())
	if err != nil {
		return time.Now(), fmt.Errorf("failed to fetch %s: %v", jsonURL.String(), err)
	}
	defer resp.Body.Close()

	body, readErr := ioutil.ReadAll(resp.Body)
	if readErr != nil {
		return time.Now(), fmt.Errorf("failed to read body at %s: %v", jsonURL.String(), err)
	}

	var prowInfo ProwJSON
	err = json.Unmarshal(body, &prowInfo)
	if err != nil {
		return time.Now(), fmt.Errorf("failed to unmarshal json %s: %v", body, err)
	}

	return time.Unix(int64(prowInfo.Timestamp), 0), nil
}

func fetchAuditLogsFromProwJob(logger *logrus.Logger, prowJobURL *url.URL) (string, error) {
	tmpDir, err := os.MkdirTemp("", "audit-span")
	if err != nil {
		return "", err
	}

	prowjobInfo, err := fetchAuditLogsTar(logger, prowJobURL)
	if err != nil {
		return "", err
	}

	auditLogArchiveSplit := strings.Split(prowjobInfo.AuditLogsTarURL, "/")
	auditLogArchiveFilename := auditLogArchiveSplit[len(auditLogArchiveSplit)-1]

	auditLogPath := filepath.Join(tmpDir, auditLogArchiveFilename)
	logger.WithFields(logrus.Fields{"url": prowjobInfo.AuditLogsTarURL, "path": auditLogPath}).Info("Downloading audit logs")

	g := got.New()
	if err = g.Download(prowjobInfo.AuditLogsTarURL, auditLogPath); err != nil {
		return "", err
	}
	// Unpack audit tar.gzs from audit-tar
	extractedArchives, err := untarIt(logger, tmpDir, auditLogPath)
	if err != nil {
		return tmpDir, err
	}
	if err := os.Remove(auditLogPath); err != nil {
		return tmpDir, err
	}

	// Ungz each file there too
	errs := []error{}
	extractedLogFiles := []string{}
	for _, auditTarGz := range extractedArchives {
		extractedFile, err := unGzIt(logger, auditTarGz)
		if err != nil {
			errs = append(errs, err)
		}
		extractedLogFiles = append(extractedLogFiles, extractedFile)
		if err := os.Remove(auditTarGz); err != nil {
			errs = append(errs, err)
		}
	}
	err = errors.Join(errs...)
	if err != nil {
		return tmpDir, err
	}

	// List files in tmp dir and extact them all with no filter
	logger.WithFields(logrus.Fields{"files": len(extractedLogFiles), "dir": tmpDir}).Info("Extracted files")
	return tmpDir, nil
}

func findAuditLogsInDir(logger *logrus.Logger, auditLogDir string) ([]string, error) {
	foundFiles := []string{}
	err := filepath.WalkDir(auditLogDir, func(path string, di fs.DirEntry, err error) error {
		if !strings.Contains(path, ".log") {
			return nil
		}
		foundFiles = append(foundFiles, path)
		return nil
	})
	logger.WithFields(logrus.Fields{"files": len(foundFiles), "dir": auditLogDir}).Info("Found log files")
	return foundFiles, err
}

func unGzIt(logger *logrus.Logger, mpath string) (string, error) {
	logger.WithFields(logrus.Fields{"file": mpath}).Info("Ungzipping")
	fr, err := read(mpath)
	if err != nil {
		return "", err
	}
	defer fr.Close()
	gr, err := gzip.NewReader(fr)
	if err != nil {
		return "", err
	}

	cwd := filepath.Dir(mpath)
	newFileName := strings.ReplaceAll(filepath.Base(mpath), ".gz", "")
	localPath := filepath.Join(cwd, newFileName)
	ow, err := overwrite(localPath)
	if err != nil {
		return localPath, err
	}
	defer ow.Close()
	if _, err := io.Copy(ow, gr); err != nil {
		return localPath, err
	}
	return localPath, nil
}

func untarIt(logger *logrus.Logger, tmpDir string, mpath string) ([]string, error) {
	result := []string{}

	logger.WithFields(logrus.Fields{"file": mpath}).Infof("Untarring")
	fr, err := read(mpath)
	if err != nil {
		return result, err
	}
	defer fr.Close()
	gr, err := gzip.NewReader(fr)
	if err != nil {
		return result, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	errs := []error{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return result, err
		}
		path := hdr.Name
		if !strings.Contains(path, "-audit") {
			continue
		}

		splitPath := strings.Split(path, "/")
		if len(splitPath) < 2 {
			continue
		}
		subDirName := splitPath[len(splitPath)-2]
		subDirPath := filepath.Join(tmpDir, subDirName)
		_, err = os.Stat(subDirPath)
		if os.IsNotExist(err) {
			if err := os.Mkdir(subDirPath, 0755); err != nil {
				errs = append(errs, err)
				continue
			}
		}

		localPath := filepath.Join(subDirPath, filepath.Base(path))
		logger.WithFields(logrus.Fields{"file": path, "destination": localPath}).Infof("Extracting")
		ow, err := overwrite(localPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		defer ow.Close()
		if _, err := io.Copy(ow, tr); err != nil {
			errs = append(errs, err)
			continue
		}
		result = append(result, localPath)
	}
	return result, errors.Join(errs...)
}

func read(mpath string) (*os.File, error) {
	f, err := os.OpenFile(mpath, os.O_RDONLY, 0444)
	if err != nil {
		return f, err
	}
	return f, nil
}

func overwrite(mpath string) (*os.File, error) {
	f, err := os.OpenFile(mpath, os.O_RDWR|os.O_TRUNC, 0777)
	if err != nil {
		f, err = os.Create(mpath)
		if err != nil {
			return f, err
		}
	}
	return f, nil
}
