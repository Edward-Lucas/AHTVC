package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	// 외부 라이브러리 임포트 없음
)

// AutoEQ 데이터를 저장할 구조체
type AutoEQData map[int]float64

// 추가할 EQ 포인트를 위한 구조체 (정렬을 위해 필요)
type eqPoint struct {
	Freq int
	Gain float64
}

// --- 상수 정의 ---

// Harman -> VDSF 변환 EQ 데이터 (실제 값 포함)
var harmanToVdsfEQ = parseConstantEQ(`
GraphicEQ: 20 -0.7; 21 -0.8; 22 -0.9; 23 -1.0; 24 -1.1; 26 -1.2; 27 -1.3; 29 -1.3; 30 -1.4; 32 -1.4; 34 -1.4; 36 -1.3; 38 -1.2; 40 -1.1; 43 -1.0; 45 -0.9; 48 -0.8; 50 -0.7; 53 -0.6; 56 -0.3; 59 -0.2; 63 -0.0; 66 0.2; 70 0.3; 74 0.5; 78 0.8; 83 1.0; 87 1.2; 92 1.4; 97 1.7; 103 1.9; 109 2.3; 115 2.6; 121 2.7; 128 3.0; 136 3.3; 143 3.5; 151 3.7; 160 3.8; 169 4.0; 178 4.0; 188 4.1; 199 4.1; 210 4.2; 222 4.3; 235 4.4; 248 4.5; 262 4.6; 277 4.7; 292 4.7; 309 4.8; 326 4.7; 345 4.7; 364 4.7; 385 4.7; 406 4.7; 429 4.7; 453 4.7; 479 4.8; 506 4.9; 534 4.9; 565 5.0; 596 5.0; 630 5.0; 665 5.1; 703 5.0; 743 5.0; 784 4.9; 829 4.8; 875 4.7; 924 4.6; 977 4.6; 1032 4.5; 1090 4.3; 1151 4.3; 1216 4.2; 1284 4.1; 1357 4.1; 1433 4.0; 1514 3.9; 1599 3.9; 1689 3.9; 1784 3.8; 1885 3.7; 1991 3.7; 2103 3.6; 2221 3.5; 2347 3.5; 2479 3.4; 2618 3.4; 2766 3.2; 2921 3.2; 3086 3.0; 3260 2.9; 3443 2.7; 3637 2.4; 3842 2.2; 4058 1.9; 4287 1.7; 4528 1.4; 4783 1.0; 5052 0.8; 5337 0.5; 5637 0.2; 5955 0.0; 6290 -0.1; 6644 -0.2; 7018 0.0; 7414 0.1; 7831 0.5; 8272 1.2; 8738 2.7; 9230 4.2; 9749 5.4; 10298 5.3; 10878 4.4; 11490 4.0; 12137 4.2; 12821 4.7; 13543 5.3; 14305 5.5; 15110 5.1; 15961 4.6; 16860 4.2; 17809 4.0; 18812 3.9; 19871 3.9
`)

// 추가할 EQ 설정 (Wavelet EQ)
var x2EQPoints = func() []eqPoint {
	points := []eqPoint{
		{Freq: 62, Gain: 1.6}, {Freq: 125, Gain: 0.4}, {Freq: 250, Gain: -0.6},
		{Freq: 500, Gain: 0.0}, {Freq: 1000, Gain: -0.4}, {Freq: 2000, Gain: -0.7},
		{Freq: 4000, Gain: -0.5}, {Freq: 8000, Gain: -0.1}, {Freq: 16000, Gain: 0.3},
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Freq < points[j].Freq })
	return points
}()

// 이동 평균 스무딩 파라미터
const (
	smoothStartFreq     = 8000.0 // 스무딩 시작 주파수
	movingAverageWindow = 5      // 이동 평균 창 크기 (홀수 권장, 클수록 부드러움)
)

// HTML 템플릿
var indexTemplate = template.Must(template.New("").Parse(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>AutoEQ Harman to VDSF Converter (AHTVC)</title>
     <style>
        body { font-family: sans-serif; padding: 20px; max-width: 800px; margin: auto; background-color: #f0f0f0; color: #333; }
        h1 { color: #1a1a1a; border-bottom: 2px solid #ccc; padding-bottom: 10px; }
		p { line-height: 1.6; }
        label { display: block; margin-top: 15px; font-weight: bold; color: #555; }
        input[type=file] { margin-top: 5px; padding: 8px; border: 1px solid #ccc; border-radius: 4px; background-color: #fff; }
		input[type=submit] { padding: 10px 20px; background-color: #007bff; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 1em; margin-top: 15px; transition: background-color 0.2s; }
		input[type=submit]:hover { background-color: #0056b3; }
        textarea { width: 95%; height: 150px; margin-top: 10px; font-family: monospace; white-space: pre; overflow-wrap: normal; overflow-x: scroll; display: block; border: 1px solid #ccc; border-radius: 4px; padding: 10px; background-color: #fff; }
        .error { color: #D8000C; margin-top: 15px; border: 1px solid #D8000C; padding: 15px; background-color: #FFD2D2; border-radius: 4px; }
        .result-container { margin-top: 25px; }
        .result-box { margin-bottom: 25px; padding: 20px; border: 1px solid #ccc; background-color: #fff; border-radius: 4px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
		.filename { font-weight: bold; font-family: monospace; margin-bottom: 10px; font-size: 1.1em; color: #333; }
		.action-buttons button { margin-right: 10px; cursor: pointer; font-size: 0.9em; padding: 5px 10px; margin-top: 10px; border: 1px solid #ccc; background-color: #eee; border-radius: 4px; transition: background-color 0.2s; }
        .action-buttons button:hover { background-color: #ddd; }
		.copy-feedback { font-size: 0.8em; color: green; margin-left: 5px; display: none; font-weight: bold; }
    </style>
	<script>
		function copyToClipboard(elementId, feedbackId) {
			var copyText = document.getElementById(elementId);
			if (!copyText) return;
			copyText.select();
			copyText.setSelectionRange(0, 99999);
			try {
				navigator.clipboard.writeText(copyText.value).then(function() {
					var feedback = document.getElementById(feedbackId);
					if (feedback) { feedback.style.display = 'inline'; setTimeout(function() { feedback.style.display = 'none'; }, 1500); }
				}, function(err) {
					console.error('클립보드 복사 실패:', err);
					try {
						var successful = document.execCommand('copy');
						if (successful) { var feedback = document.getElementById(feedbackId); if (feedback) { feedback.style.display = 'inline'; setTimeout(function() { feedback.style.display = 'none'; }, 1500); }
						} else { alert('클립보드 복사에 실패했습니다.'); }
					} catch (errFallback) { alert('클립보드 복사에 실패했습니다.'); console.error('Fallback copy command failed:', errFallback); }
				});
			} catch (errGlobal) { alert('클립보드 API를 사용할 수 없습니다.'); console.error('Clipboard API error:', errGlobal); }
		}
		function downloadTextFile(filename, text) {
			if (typeof filename !== 'string' || typeof text !== 'string') { console.error("Download Error: Invalid types", filename, text); alert("파일 다운로드 오류: 타입 오류"); return; }
			var element = document.createElement('a');
			var blob = new Blob([text], {type: 'text/plain;charset=utf-8'});
			var url = URL.createObjectURL(blob);
			element.setAttribute('href', url);
			element.setAttribute('download', filename);
			element.style.display = 'none';
			document.body.appendChild(element);
			element.click();
			document.body.removeChild(element);
			URL.revokeObjectURL(url);
		}
        function handleDownloadClick(event) {
            const button = event.target;
            const filename = button.dataset.filename;
            const content = button.dataset.content;
            if (filename && content) { downloadTextFile(filename, content); } else { console.error("Download Error: Missing data attrs", button); alert("파일 다운로드 오류: 데이터 없음"); }
        }
	</script>
</head>
<body>
    <h1>AutoEQ Harman to VDSF Converter (AHTVC)</h1>
    <p>이어폰/헤드폰의 <b>Harman 타겟 AutoEQ 파일</b>을 업로드하세요.</p>
	<p>자동으로 VDSF 타겟 기반의 EQ 파일을 생성합니다 (For Wavelet).</p>
    <form method="POST" enctype="multipart/form-data">
        <label for="sourceHarmanFile">Harman 타겟 EQ 파일 (.txt):</label>
        <input type="file" id="sourceHarmanFile" name="sourceHarmanFile" accept=".txt" required>
        <br><br>
        <input type="submit" value="변환하기">
    </form>
    <div class="result-container">
		{{if .Filename1}}
        <div class="result-box">
            <div class="filename">{{.Filename1}}</div>
            <textarea id="result1Text" readonly>{{.Result1}}</textarea>
			<div class="action-buttons">
				<button type="button" onclick="copyToClipboard('result1Text', 'copyFeedback1')">클립보드 복사</button>
				<span class="copy-feedback" id="copyFeedback1">복사됨!</span>
				<button type="button" data-filename="{{.Filename1}}" data-content="{{.Result1}}" onclick="handleDownloadClick(event)">파일로 저장 (.txt)</button>
			</div>
        </div>
        {{end}}
        {{if .Filename2}}
        <div class="result-box">
             <div class="filename">{{.Filename2}}</div>
           <textarea id="result2Text" readonly>{{.Result2}}</textarea>
		   <div class="action-buttons">
				<button type="button" onclick="copyToClipboard('result2Text', 'copyFeedback2')">클립보드 복사</button>
				<span class="copy-feedback" id="copyFeedback2">복사됨!</span>
				<button type="button" data-filename="{{.Filename2}}" data-content="{{.Result2}}" onclick="handleDownloadClick(event)">파일로 저장 (.txt)</button>
		   </div>
        </div>
        {{end}}
    </div>
    {{if .Error}} <div class="error"> <strong>오류:</strong> <pre>{{.Error}}</pre> </div> {{end}}
</body>
</html>
`))

// main 함수
func main() {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("포트 찾기 실패: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	address := fmt.Sprintf("http://localhost:%d", port)
	listener.Close()

	http.HandleFunc("/", handleConvert)
	fmt.Printf("서버 주소: %s\n", address)
	fmt.Println("웹 브라우저 여는 중...")

	go func() {
		time.Sleep(1 * time.Second)
		err := openBrowser(address)
		if err != nil {
			fmt.Printf("브라우저 열기 오류: %v\n", err)
		}
	}()

	log.Printf("포트 %d 에서 서버 시작...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("서버 시작 실패: %v", err)
	}
	log.Println("서버 종료.")
}

// 웹 요청 처리 핸들러
func handleConvert(w http.ResponseWriter, r *http.Request) {
	resultData := map[string]interface{}{}

	if r.Method == http.MethodPost {
		var sourceName string = "UnknownDevice"

		sourceHarmanFile, sourceHarmanHandler, errH := r.FormFile("sourceHarmanFile")
		if errH != nil {
			resultData["Error"] = "파일 업로드 오류: " + errH.Error()
			w.WriteHeader(http.StatusBadRequest)
			indexTemplate.Execute(w, resultData)
			return
		}
		defer sourceHarmanFile.Close()
		sourceName = extractSourceName(sourceHarmanHandler.Filename)

		sourceHarmanBytes, errHRead := io.ReadAll(sourceHarmanFile)
		if errHRead != nil {
			resultData["Error"] = "파일 읽기 오류: " + errHRead.Error()
			w.WriteHeader(http.StatusInternalServerError)
			indexTemplate.Execute(w, resultData)
			return
		}

		sourceHarmanData, errHParse := parseAutoEQ(string(sourceHarmanBytes))
		if errHParse != nil {
			resultData["Error"] = fmt.Sprintf("입력 파일 파싱 오류: %v\n입력 파일 내용을 확인해주세요.", errHParse)
			w.WriteHeader(http.StatusBadRequest)
			indexTemplate.Execute(w, resultData)
			return
		}

		// --- 계산 로직 ---
		calculated_S_to_V_EQ := make(AutoEQData)
		allFreqsMap := make(map[int]bool)
		for freq := range sourceHarmanData {
			allFreqsMap[freq] = true
		}
		for freq := range harmanToVdsfEQ {
			allFreqsMap[freq] = true
		}
		var allFreqs []int
		for freq := range allFreqsMap {
			allFreqs = append(allFreqs, freq)
		}
		sort.Ints(allFreqs)
		for _, freq := range allFreqs {
			calculated_S_to_V_EQ[freq] = sourceHarmanData[freq] + harmanToVdsfEQ[freq]
		}

		// --- 스무딩 1단계 (이동 평균 사용) ---
		smoothed_S_to_V_EQ := applyMovingAverageSmoothing(calculated_S_to_V_EQ, allFreqs, movingAverageWindow, smoothStartFreq)
		fmt.Println("1차 스무딩 적용됨.")

		// --- 결과 1 생성 (스무딩 후 NoPreamp) ---
		result1EQ_NoPreamp := applyNoPreamp(smoothed_S_to_V_EQ, allFreqs)
		filename1 := fmt.Sprintf("%s_AHTVC-By_MiFun.txt", sourceName)
		result1Str := formatEQString(result1EQ_NoPreamp, allFreqs)
		resultData["Filename1"] = filename1
		resultData["Result1"] = result1Str

		// --- 결과 2 생성 (스무딩 + X2 + 스무딩 + NoPreamp) ---
		intermediateResult2EQ_withX2 := applyX2EQ(smoothed_S_to_V_EQ, x2EQPoints, allFreqs)
		smoothed_IntermediateResult2EQ := applyMovingAverageSmoothing(intermediateResult2EQ_withX2, allFreqs, movingAverageWindow, smoothStartFreq)
		fmt.Println("2차 스무딩 적용됨.")
		result2EQ_NoPreamp := applyNoPreamp(smoothed_IntermediateResult2EQ, allFreqs)
		filename2 := fmt.Sprintf("%s_AHTVCLr2-By_MiFun.txt", sourceName)
		result2Str := formatEQString(result2EQ_NoPreamp, allFreqs)
		resultData["Filename2"] = filename2
		resultData["Result2"] = result2Str
	}

	// 템플릿 렌더링
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := indexTemplate.Execute(w, resultData)
	if err != nil {
		log.Printf("템플릿 실행 오류: %v", err)
		http.Error(w, "페이지 렌더링 오류", http.StatusInternalServerError)
	}
}

// --- Helper Functions ---

// 상수 EQ 파싱
func parseConstantEQ(content string) AutoEQData {
	data, err := parseAutoEQ(content)
	if err != nil {
		log.Fatalf("상수 EQ 데이터 '%s...' 파싱 실패: %v", content[:min(30, len(content))], err)
	}
	return data
}

// AutoEQ 파일 파싱
func parseAutoEQ(content string) (AutoEQData, error) {
	data := make(AutoEQData)
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	graphicEqFound := false
	lineNum := 0

	for _, line := range lines {
		lineNum++
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}

		if strings.HasPrefix(line, "GraphicEQ:") {
			graphicEqFound = true
			pointsStr := strings.TrimPrefix(line, "GraphicEQ:")
			pointsStr = strings.TrimSpace(pointsStr)
			points := strings.Split(pointsStr, ";")
			pointCount := 0
			for pointIdx, point := range points {
				point = strings.TrimSpace(point)
				if point == "" {
					if pointIdx == len(points)-1 {
						continue
					} else {
						fmt.Printf("경고: Line %d, 비어있는 EQ 포인트 발견 (인덱스 %d).\n", lineNum, pointIdx)
						continue
					}
				}
				parts := strings.Fields(point)
				if len(parts) != 2 {
					fmt.Printf("경고: Line %d, 잘못된 포인트 형식 무시 (항목 %d개): '%s'\n", lineNum, len(parts), point)
					continue
				}
				freq, errF := strconv.Atoi(parts[0])
				gain, errG := strconv.ParseFloat(parts[1], 64)
				if errF != nil || errG != nil {
					fmt.Printf("경고: Line %d, 숫자 변환 오류 무시 ('%s'): %v, %v\n", lineNum, point, errF, errG)
					continue
				}
				if math.IsNaN(gain) || math.IsInf(gain, 0) {
					fmt.Printf("경고: Line %d, 잘못된 게인 값 (NaN or Inf) 무시 at freq %d\n", lineNum, freq)
					continue
				}
				if freq <= 0 || freq > 30000 {
					fmt.Printf("경고: Line %d, 비정상적인 주파수 값 무시: %d\n", lineNum, freq)
					continue
				}
				data[freq] = gain
				pointCount++
			}
			if pointCount == 0 && len(points) > 0 && strings.TrimSpace(points[0]) != "" {
				return nil, fmt.Errorf("line %d: GraphicEQ 라인에서 유효한 포인트를 찾을 수 없음: '%s'", lineNum, line)
			}
			break
		}
	}

	if !graphicEqFound {
		return nil, errors.New("'GraphicEQ:' 라인을 찾을 수 없음 (파일 형식을 확인하세요)")
	}
	if len(data) == 0 {
		return nil, errors.New("파싱된 유효한 EQ 데이터 포인트가 없음")
	}
	return data, nil
}

// Preamp 제거
func applyNoPreamp(inputEQ AutoEQData, sortedFreqs []int) AutoEQData {
	outputEQ := make(AutoEQData)
	maxGain := -math.MaxFloat64
	hasData := false

	for _, freq := range sortedFreqs {
		if gain, ok := inputEQ[freq]; ok {
			if math.IsNaN(gain) || math.IsInf(gain, 0) {
				fmt.Printf("경고: applyNoPreamp 입력에서 잘못된 게인 값 발견 (freq: %d). 0.0으로 처리.\n", freq)
				gain = 0.0
			}
			outputEQ[freq] = gain
			if gain > maxGain {
				maxGain = gain
			}
			hasData = true
		}
	}

	if !hasData {
		fmt.Println("applyNoPreamp 경고: 처리할 유효한 EQ 데이터가 없습니다.")
		return outputEQ
	}
	if math.IsNaN(maxGain) || math.IsInf(maxGain, 0) {
		fmt.Println("경고: 최대 게인 계산 불가 (NaN or Inf). Preamp 이동이 적용되지 않습니다.")
		return outputEQ
	}

	shift := 0.0
	if maxGain > 1e-9 {
		shift = maxGain
	}

	for freq := range outputEQ {
		newValue := outputEQ[freq] - shift
		if math.IsNaN(newValue) || math.IsInf(newValue, 0) {
			fmt.Printf("경고: Preamp 적용 중 잘못된 값 발생 (freq: %d). 0.0으로 대체.\n", freq)
			outputEQ[freq] = 0.0
		} else {
			outputEQ[freq] = newValue
		}
	}
	return outputEQ
}

// 문자열 포맷
func formatEQString(eqData AutoEQData, sortedFreqs []int) string {
	var resultBuffer bytes.Buffer
	resultBuffer.WriteString("GraphicEQ: ")
	var eqPoints []string
	for _, freq := range sortedFreqs {
		if gain, ok := eqData[freq]; ok {
			if math.IsNaN(gain) || math.IsInf(gain, 0) {
				fmt.Printf("경고: 결과 포맷팅 중 잘못된 게인 값 발견 (freq: %d). 0.0으로 대체.\n", freq)
				gain = 0.0
			}
			eqPoints = append(eqPoints, fmt.Sprintf("%d %.1f", freq, gain))
		}
	}
	if len(eqPoints) > 0 {
		resultBuffer.WriteString(strings.Join(eqPoints, "; "))
	} else {
		resultBuffer.Reset()
		resultBuffer.WriteString("GraphicEQ: (No valid points)")
	}
	return resultBuffer.String()
}

// 소스 이름 추출
func extractSourceName(filename string) string {
	name := strings.TrimSuffix(filename, ".txt")
	patternsToRemove := []string{" Graphic Filters Harman", " Graphic Filters VDSF", " Graphic Filters", " target Harman", " target VDSF", " target", " (AVG)", " (Target)", "(L)", "(R)", " Harman", " VDSF"}
	normalizedName := name
	changed := true
	for changed {
		changed = false
		currentName := normalizedName
		for _, pattern := range patternsToRemove {
			lowerPattern := strings.ToLower(pattern)
			lowerName := strings.ToLower(currentName)
			if strings.HasSuffix(lowerName, lowerPattern) {
				normalizedName = strings.TrimSpace(currentName[:len(currentName)-len(pattern)])
				changed = true
				break
			}
		}
	}
	normalizedName = strings.TrimSpace(normalizedName)
	if normalizedName == "" {
		return "UnknownDevice"
	}
	lowerFinalName := strings.ToLower(normalizedName)
	if lowerFinalName == "result" || lowerFinalName == "output" || lowerFinalName == "graphic" || lowerFinalName == "eq" {
		parts := strings.Fields(name)
		if len(parts) > 0 && strings.ToLower(parts[0]) != lowerFinalName {
			return parts[0]
		}
		return "UnknownDevice"
	}
	return normalizedName
}

// 브라우저 열기
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		found := false
		for _, c := range []string{"xdg-open", "gnome-open", "kde-open", "sensible-browser"} {
			p, err := exec.LookPath(c)
			if err == nil {
				cmd = p
				args = []string{url}
				found = true
				break
			}
		}
		if !found {
			return errors.New("지원되는 브라우저 열기 명령을 찾을 수 없음")
		}
	}
	command := exec.Command(cmd, args...)
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	err := command.Start()
	if err != nil {
		return fmt.Errorf("'%s %s' 실행 오류: %w", cmd, strings.Join(args, " "), err)
	}
	return nil
}

// X2 EQ 적용 함수
func applyX2EQ(baseEQ AutoEQData, x2EQ []eqPoint, allFreqs []int) AutoEQData {
	resultEQ := make(AutoEQData)
	for freq, gain := range baseEQ {
		resultEQ[freq] = gain
	}

	if len(x2EQ) == 0 {
		return resultEQ
	}

	for _, targetFreq := range allFreqs {
		targetFreqFloat := float64(targetFreq)
		x2GainToAdd := 0.0

		if targetFreq < x2EQ[0].Freq {
			x2GainToAdd = x2EQ[0].Gain
		} else if targetFreq > x2EQ[len(x2EQ)-1].Freq {
			x2GainToAdd = x2EQ[len(x2EQ)-1].Gain
		} else {
			for i := 0; i < len(x2EQ)-1; i++ {
				lowerPoint := x2EQ[i]
				upperPoint := x2EQ[i+1]
				if targetFreq == lowerPoint.Freq {
					x2GainToAdd = lowerPoint.Gain
					break
				}
				if targetFreq == upperPoint.Freq {
					x2GainToAdd = upperPoint.Gain
					break
				}
				if targetFreq > lowerPoint.Freq && targetFreq < upperPoint.Freq {
					logTarget := math.Log10(targetFreqFloat)
					logLower := math.Log10(float64(lowerPoint.Freq))
					logUpper := math.Log10(float64(upperPoint.Freq))
					if logUpper-logLower < 1e-9 {
						x2GainToAdd = lowerPoint.Gain
					} else {
						proportion := (logTarget - logLower) / (logUpper - logLower)
						x2GainToAdd = lowerPoint.Gain + proportion*(upperPoint.Gain-lowerPoint.Gain)
					}
					break
				}
			}
			if x2GainToAdd == 0 && targetFreq == x2EQ[len(x2EQ)-1].Freq {
				x2GainToAdd = x2EQ[len(x2EQ)-1].Gain
			}
		}

		if baseGain, ok := resultEQ[targetFreq]; ok {
			newGain := baseGain + x2GainToAdd
			if math.IsNaN(newGain) || math.IsInf(newGain, 0) {
				fmt.Printf("경고: applyX2EQ 적용 중 잘못된 값 발생 (freq: %d). 원래 값 유지.\n", targetFreq)
			} else {
				resultEQ[targetFreq] = newGain
			}
		}
	}
	return resultEQ
}

// 이동 평균 스무딩 함수
func applyMovingAverageSmoothing(inputEQ AutoEQData, sortedFreqs []int, windowSize int, startFreq float64) AutoEQData {
	outputEQ := make(AutoEQData)
	for freq, gain := range inputEQ {
		outputEQ[freq] = gain
	}

	if windowSize <= 1 || windowSize%2 == 0 {
		fmt.Printf("경고: 이동 평균 윈도우 크기(%d)는 1보다 큰 홀수여야 합니다. 스무딩을 건너<0xEB><0x9C><0x84>니다.\n", windowSize)
		return outputEQ
	}
	if len(sortedFreqs) < windowSize {
		fmt.Printf("경고: 데이터 포인트 개수(%d)가 윈도우 크기(%d)보다 작아 스무딩을 건너<0xEB><0x9C><0x84>니다.\n", len(sortedFreqs), windowSize)
		return outputEQ
	}

	halfWindow := windowSize / 2
	startIndex := -1
	for i, freq := range sortedFreqs {
		if float64(freq) >= startFreq {
			startIndex = i
			break
		}
	}

	if startIndex == -1 || len(sortedFreqs)-startIndex < windowSize {
		if startIndex == -1 {
			fmt.Printf("%f Hz 이상 포인트를 찾지 못해 스무딩을 건너<0xEB><0x9C><0x84>니다.\n", startFreq)
		} else {
			fmt.Printf("%f Hz 이상 데이터 포인트(%d)가 윈도우 크기(%d)보다 작아 스무딩을 건너<0xEB><0x9C><0x84>니다.\n", startFreq, len(sortedFreqs)-startIndex, windowSize)
		}
		return outputEQ
	}

	fmt.Printf("%f Hz 이상 고음역대에 이동 평균 스무딩 적용 (Window=%d)...\n", startFreq, windowSize)

	tempGains := make([]float64, len(sortedFreqs))
	for i, freq := range sortedFreqs {
		if gain, ok := inputEQ[freq]; ok {
			tempGains[i] = gain
		} else {
			tempGains[i] = 0.0
		}
	}

	// 이동 평균 계산 (가장자리 처리는 원본 유지 방식)
	smoothedOutputGains := make([]float64, len(sortedFreqs))
	copy(smoothedOutputGains, tempGains) // 원본으로 초기화

	for i := startIndex + halfWindow; i < len(sortedFreqs)-halfWindow; i++ {
		sum := 0.0
		count := 0
		for j := i - halfWindow; j <= i+halfWindow; j++ {
			if j >= 0 && j < len(tempGains) {
				if !math.IsNaN(tempGains[j]) && !math.IsInf(tempGains[j], 0) {
					sum += tempGains[j]
					count++
				} else {
					fmt.Printf("경고: 스무딩 계산 중 잘못된 값 발견 (index: %d). 건너<0xEB><0x9C><0x84>뜁니다.\n", j)
				}
			}
		}
		if count > 0 {
			avg := sum / float64(count)
			if !math.IsNaN(avg) && !math.IsInf(avg, 0) {
				smoothedOutputGains[i] = avg // 계산된 평균값으로 업데이트
			} else {
				fmt.Printf("경고: 스무딩 평균 계산 결과가 잘못됨 (index: %d). 원래 값 유지.\n", i)
			}
		} else {
			fmt.Printf("경고: 스무딩 윈도우 내 유효 값 없음 (index: %d). 원래 값 유지.\n", i)
		}
	}

	// 스무딩된 결과를 원래의 map 구조로 변환
	for i, freq := range sortedFreqs {
		outputEQ[freq] = smoothedOutputGains[i]
	}

	return outputEQ
}

// min 함수
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
