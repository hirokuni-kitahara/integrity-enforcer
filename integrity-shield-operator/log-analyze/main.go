package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const timestampFormat = "2006-01-02T15:04:05.999999999Z"

type LogRecord struct {
	Allow      bool   `json:"allow,omitempty"`
	UID        string `json:"UID,omitempty"`
	Operation  string `json:"operation,omitempty"`
	UserName   string `json:"userName,omitempty"`
	RequestUID string `json:"requestUID,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Level      string `json:"level,omitempty"`
	Msg        string `json:"msg,omitempty"`
	Time       string `json:"time,omitempty"`
}

func (lr LogRecord) GetUID() string {
	uid := ""
	if strings.HasPrefix(lr.Msg, "request parsed") {
		parts := strings.Split(lr.Msg, ", UID: ")
		uid = parts[1]
	} else if strings.HasPrefix(lr.Msg, "returning a response") {
		parts := strings.Split(lr.Msg, ", UID: ")
		uid = parts[1]
	} else {
		uid = lr.RequestUID
	}
	return uid
}

type RequestRecord struct {
	UID                         string      `json:"UID,omitempty"`
	RequestParsed               time.Time   `json:"requestParsed,omitempty"`
	ProcessNewRequest           time.Time   `json:"processNewRequest,omitempty"`
	VerifyOption                time.Time   `json:"verifyOption,omitempty"`
	PrepareVerifyResourceDetail []time.Time `json:"prepareVerifyResourceDetail,omitempty"`
	FetchingManifest            time.Time   `json:"fetchingManifest,omitempty"`
	MatchingManifest            time.Time   `json:"matchingManifest,omitempty"`
	MatchingManifestDetail      []time.Time `json:"matchingManifestDetail,omitempty"`
	VerifyingSignature          time.Time   `json:"verifyingSignature,omitempty"`
	VerifyResourceResult        time.Time   `json:"verifyResourceResult,omitempty"`
	ResultDecided               time.Time   `json:"resultDecided,omitempty"`
	EventReported               time.Time   `json:"eventReported,omitempty"`
	ReturningResponse           time.Time   `json:"returningResponse,omitempty"`
}

type TimeRecord struct {
	Timestamp                   time.Time       `json:"timestamp"`
	Total                       time.Duration   `json:"total"`
	LoadRequestHandlerConfig    time.Duration   `json:"loadRequestHandlerConfig"`
	PrepareVerifyResource       time.Duration   `json:"prepareVerifyResource"`
	PrepareVerifyResourceDetail []time.Duration `json:"prepareVerifyResourceDetail"`
	PrepareManifestFetch        time.Duration   `json:"prepareManifestFetch"`
	ManifestFetch               time.Duration   `json:"manifestFetch"`
	DryRunForManifestMatch      time.Duration   `json:"dryRunForManifestMatch"`
	VerifySignature             time.Duration   `json:"verifySignature"`
	FinalizeDecisionResult      time.Duration   `json:"finalizeDecisionResult"`
	EventReport                 time.Duration   `json:"eventReport"`
	FinalizeResponse            time.Duration   `json:"finalizeResponse"`
}

func req2time(r *RequestRecord) *TimeRecord {
	pvrDetails := []time.Duration{}
	for i := range r.PrepareVerifyResourceDetail {
		if i == len(r.PrepareVerifyResourceDetail)-2 {
			break
		}
		now := r.PrepareVerifyResourceDetail[i]
		next := r.PrepareVerifyResourceDetail[i+1]
		pvrDetails = append(pvrDetails, next.Sub(now))
	}
	t := &TimeRecord{
		Timestamp:                   r.RequestParsed,
		Total:                       r.ReturningResponse.Sub(r.RequestParsed),
		LoadRequestHandlerConfig:    r.ProcessNewRequest.Sub(r.RequestParsed),
		PrepareVerifyResource:       r.VerifyOption.Sub(r.ProcessNewRequest),
		PrepareVerifyResourceDetail: pvrDetails,
		PrepareManifestFetch:        r.FetchingManifest.Sub(r.VerifyOption),
		ManifestFetch:               r.MatchingManifest.Sub(r.FetchingManifest),
		DryRunForManifestMatch:      r.VerifyingSignature.Sub(r.MatchingManifest),
		VerifySignature:             r.VerifyResourceResult.Sub(r.VerifyingSignature),
		FinalizeDecisionResult:      r.ResultDecided.Sub(r.VerifyResourceResult),
		EventReport:                 r.EventReported.Sub(r.ResultDecided),
		FinalizeResponse:            r.ReturningResponse.Sub(r.EventReported),
	}
	return t
}

func parseTime(tsStr string) time.Time {
	t, _ := time.Parse(timestampFormat, tsStr)
	return t
}

func setRecordValue(l LogRecord, r *RequestRecord) {
	msg := l.Msg
	if strings.HasPrefix(msg, "request parsed") {
		r.RequestParsed = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "Process new request") {
		r.ProcessNewRequest = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "VerifyOption:") {
		r.VerifyOption = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "PrepareVerifyResource:") {
		r.PrepareVerifyResourceDetail = append(r.PrepareVerifyResourceDetail, parseTime(l.Time))
	} else if strings.HasPrefix(msg, "fetching manifest") {
		r.FetchingManifest = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "matching object") {
		r.MatchingManifest = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "try matching with") {
		r.MatchingManifestDetail = append(r.MatchingManifestDetail, parseTime(l.Time))
	} else if strings.HasPrefix(msg, "verifying signature") {
		r.VerifyingSignature = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "VerifyResource") {
		r.VerifyResourceResult = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "result decided") {
		r.ResultDecided = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "event reported") {
		r.EventReported = parseTime(l.Time)
	} else if strings.HasPrefix(msg, "returning a response") {
		r.ReturningResponse = parseTime(l.Time)
	}
}

func constains(uid string, records []*RequestRecord) (bool, int) {
	for i, rr := range records {
		if rr.UID == uid {
			return true, i
		}
	}
	return false, -1
}

func readLines(fname string) []string {
	file, err := os.Open(fname)
	if err != nil {
		log.Fatalf("failed opening file: %s", err)
	}

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var txtlines []string
	for scanner.Scan() {
		txtlines = append(txtlines, scanner.Text())
	}
	file.Close()
	return txtlines
}

func main() {
	fname := os.Args[1]
	csvMode := false
	if len(os.Args) >= 3 {
		if os.Args[2] == "csv" {
			csvMode = true
		}
	}

	lines := readLines(fname)

	logRecords := []LogRecord{}
	for _, l := range lines {
		var lr LogRecord
		err := json.Unmarshal([]byte(l), &lr)
		if err != nil {
			continue
		}
		logRecords = append(logRecords, lr)
	}

	records := []*RequestRecord{}
	for _, lr := range logRecords {
		uid := lr.GetUID()
		if uid == "" {
			continue
		}
		found, idx := constains(uid, records)
		var rr *RequestRecord
		if found {
			rr = records[idx]
		} else {
			rr = &RequestRecord{
				UID: uid,
			}
		}
		setRecordValue(lr, rr)
		if found {
			records[idx] = rr
		} else {
			records = append(records, rr)
		}
	}

	tRecords := []*TimeRecord{}
	for _, r := range records {
		tRecords = append(tRecords, req2time(r))
	}

	if csvMode {
		for _, t := range tRecords {
			ts := float64(t.Timestamp.UTC().UnixNano()) / 1e9
			total := float64(t.Total.Nanoseconds()) / 1e6
			loadRequestHandlerConfig := float64(t.LoadRequestHandlerConfig.Nanoseconds()) / 1e6
			prepareVerifyResource := float64(t.PrepareVerifyResource.Nanoseconds()) / 1e6
			prepareManifestFetch := float64(t.PrepareManifestFetch.Nanoseconds()) / 1e6
			manifestFetch := float64(t.ManifestFetch.Nanoseconds()) / 1e6
			dryRunForManifestMatch := float64(t.DryRunForManifestMatch.Nanoseconds()) / 1e6
			verifySignature := float64(t.VerifySignature.Nanoseconds()) / 1e6
			finalizeDecisionResult := float64(t.FinalizeDecisionResult.Nanoseconds()) / 1e6
			eventReport := float64(t.EventReport.Nanoseconds()) / 1e6
			finalizeResponse := float64(t.FinalizeResponse.Nanoseconds()) / 1e6

			fmt.Printf("%v,%v,%v,%v,%v,%v,%v,%v,%v,%v,%v\n", ts, total, loadRequestHandlerConfig, prepareVerifyResource, prepareManifestFetch, manifestFetch, dryRunForManifestMatch, verifySignature, finalizeDecisionResult, eventReport, finalizeResponse)
		}
	} else {
		for _, t := range tRecords {
			tBytes, err := json.Marshal(t)
			if err != nil {
				log.Fatalf("failed to marshal record; error: %s", err.Error())
			}
			fmt.Println(string(tBytes))
		}
	}
}
