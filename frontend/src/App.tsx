import { useState, useCallback, useRef } from "react";
import type { AppState, UploadResponse, AnalysisResult } from "./types";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import Header from "./components/Header";
import Card from "./components/Card";
import UploadArea from "./components/UploadArea";

interface StreamEvent {
  type: string;
  content?: string;
  done?: boolean;
}

function App() {
  const [state, setState] = useState<AppState>("idle");
  const [file, setFile] = useState<File | null>(null);
  const [instructions, setInstructions] = useState("");
  const [error, setError] = useState("");
  const [result, setResult] = useState<AnalysisResult | null>(null);
  const [streamText, setStreamText] = useState("");
  const [streamStatus, setStreamStatus] = useState("");
  const sessionRef = useRef<string>("");

  const handleUpload = useCallback(async () => {
    if (!file) return;

    setError("");
    setResult(null);
    setStreamText("");
    setStreamStatus("");
    setState("uploading");

    try {
      const formData = new FormData();
      formData.append("dataset", file);
      if (instructions.trim()) {
        formData.append("instructions", instructions);
      }

      const resp = await fetch("/api/upload", {
        method: "POST",
        body: formData,
      });
      const data: UploadResponse = await resp.json();

      if (!resp.ok) {
        throw new Error(
          (data as unknown as { error: string }).error || "Upload failed",
        );
      }

      sessionRef.current = data.session_id;
      setState("analyzing");
      setStreamStatus("Starting analysis...");

      const analyzeResp = await fetch("/api/analyze/stream", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ session_id: data.session_id }),
      });

      if (!analyzeResp.ok) {
        const errData = await analyzeResp.json().catch(() => ({}));
        throw new Error(
          (errData as { error?: string }).error || "Analysis request failed",
        );
      }

      const reader = analyzeResp.body?.getReader();
      if (!reader) throw new Error("No response body");

      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";

        for (const line of lines) {
          const trimmed = line.trim();
          if (!trimmed.startsWith("data: ")) continue;

          const jsonStr = trimmed.slice(6);
          if (jsonStr === "[DONE]") continue;

          try {
            const event: StreamEvent = JSON.parse(jsonStr);

            if (event.type === "text") {
              setStreamText((prev) => prev + (event.content || ""));
              setStreamStatus("");
            } else if (event.type === "status") {
              setStreamStatus(event.content || "");
            } else if (event.type === "done") {
            } else if (event.type === "charts") {
              const chartNames: string[] = Array.isArray(event.content)
                ? (event.content as unknown as string[])
                : JSON.parse((event.content as string) || "[]");
              setResult(
                (prev): AnalysisResult => ({
                  session_id: sessionRef.current,
                  status: prev?.status || "analyzing",
                  report: prev?.report || streamText,
                  charts: chartNames,
                }),
              );
            } else if (event.type === "complete") {
              const finalReport = (event.content as string) || streamText;
              setResult(
                (prev): AnalysisResult => ({
                  session_id: sessionRef.current,
                  status: "completed",
                  report: finalReport || prev?.report || streamText,
                  charts: prev?.charts || [],
                }),
              );
              setState("done");
              setFile(null);
              setInstructions("");

              const resultsResp = await fetch(
                `/api/results?session_id=${sessionRef.current}`,
              );
              if (resultsResp.ok) {
                const fullResult: AnalysisResult = await resultsResp.json();
                setResult(fullResult);
              }
              return;
            } else if (event.type === "error") {
              throw new Error(event.content || "Analysis error");
            }
          } catch (parseErr) {
            if (parseErr instanceof SyntaxError) continue;
            throw parseErr;
          }
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "An error occurred");
      setState("error");
    }
  }, [file, instructions, streamText]);

  const handleReset = useCallback(() => {
    setResult(null);
    setStreamText("");
    setStreamStatus("");
    setState("idle");
    setError("");
    setFile(null);
    setInstructions("");
    sessionRef.current = "";
  }, []);

  const isLoading = state === "uploading" || state === "analyzing";

  return (
    <div className="app">
      <Header
        title="Data analytics"
        description="Upload your data and let the AI analyze it for you"
      />

      <Card>
        <UploadArea
          onFileSelect={setFile}
          onError={setError}
          disabled={isLoading}
          selectedFile={file}
        />

        <div className="form-group">
          <label htmlFor="instructions">Analysis instructions (optional)</label>
          <textarea
            id="instructions"
            rows={3}
            placeholder="What should the AI focus on? E.g., 'Find correlations between age and salary', 'Identify top performing categories'..."
            value={instructions}
            onChange={(e) => setInstructions(e.target.value)}
            disabled={isLoading}
          />
        </div>

        <button
          className="btn btn-primary"
          disabled={!file || isLoading}
          onClick={handleUpload}
        >
          {isLoading ? "Processing..." : "Upload & Analyze"}
        </button>

        {error && <div className="error-box">{error}</div>}
      </Card>

      {state === "analyzing" && (
        <Card>
          {streamStatus && (
            <div
              style={{
                color: "#8b949e",
                fontSize: "0.85rem",
                marginBottom: 12,
                fontStyle: "italic",
              }}
            >
              {streamStatus}
            </div>
          )}
          {streamText ? (
            <div className="report">
              <Markdown remarkPlugins={[remarkGfm]}>{streamText}</Markdown>
            </div>
          ) : (
            <div className="loading">
              <div className="spinner" />
              <p>Waiting for response...</p>
            </div>
          )}
        </Card>
      )}

      {state === "done" && result && (
        <div className="card">
          <div className="results-header">
            <h2>Analysis Results</h2>
            <button className="btn btn-secondary" onClick={handleReset}>
              New Analysis
            </button>
          </div>

          <div className="report">
            <Markdown remarkPlugins={[remarkGfm]}>{result.report}</Markdown>
          </div>

          {result.charts && result.charts.length > 0 && (
            <div className="charts-section">
              <h3>Charts</h3>
              <div className="charts-grid">
                {result.charts.map((chart) => (
                  <div key={chart} className="chart-card">
                    <img
                      src={`/charts/${chart}?session_id=${result.session_id}`}
                      alt={chart}
                      loading="lazy"
                    />
                    <div className="chart-caption">
                      {chart.replace(".png", "").replace(/_/g, " ")}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default App;
