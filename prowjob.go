package main

import (
	"encoding/json"
	"fmt"
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
	"golang.org/x/net/html"
	"k8s.io/klog/v2"
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
func fetchAuditLogsTar(url *url.URL) (ProwInfo, error) {
	prowInfo, err := getTarURLFromProw(url)
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

func getTarURLFromProw(baseURL *url.URL) (ProwInfo, error) {
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
		klog.Infof("link: %s", link)
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
		klog.Infof("link: %s", link)
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		klog.Infof("lastPathSection: %s", lastPathSegment)
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
		klog.Infof("link: %s", link)
		linkSplitBySlash := strings.Split(link, "/")
		lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
		if len(lastPathSegment) == 0 {
			lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
		}
		klog.Infof("lastPathSection: %s", lastPathSegment)
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
			klog.Infof("link: %s", link)
			linkSplitBySlash := strings.Split(link, "/")
			lastPathSegment := linkSplitBySlash[len(linkSplitBySlash)-1]
			if len(lastPathSegment) == 0 {
				lastPathSegment = linkSplitBySlash[len(linkSplitBySlash)-2]
			}
			klog.Infof("lastPathSection: %s", lastPathSegment)
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

func fetchAuditLogsFromProwJob(prowJobURL *url.URL) (string, error) {
	auditLogArchiveSplit := strings.Split(prowJobURL.Path, "/")
	auditLogArchiveFilename := auditLogArchiveSplit[len(auditLogArchiveSplit)-1]

	tmpDir, err := os.CreateTemp("audit-span", "")
	if err != nil {
		return "", err
	}

	prowjobInfo, err := fetchAuditLogsTar(prowJobURL)
	if err != nil {
		return "", err
	}
	klog.Infof("Downloading %s to %s", prowjobInfo.AuditLogsTarURL, tmpDir.Name())

	g := got.New()
	if err = g.Download(prowjobInfo.AuditLogsTarURL, tmpDir.Name()); err != nil {
		return "", err
	}

	auditLogPath := filepath.Join(tmpDir.Name(), auditLogArchiveFilename)
	// Untar audit log there
	return auditLogPath, nil
}

func findAuditLogsInDir(auditLogDir string) ([]string, error) {
	return []string{}, nil
}
