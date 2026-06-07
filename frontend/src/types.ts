export interface UploadResponse {
  session_id: string;
  filename: string;
  summary: string;
  status: string;
}

export interface AnalysisStatus {
  session_id: string;
  status: string;
  progress: string;
  error?: string;
}

export interface AnalysisResult {
  session_id: string;
  status: string;
  report: string;
  charts: string[];
  filename?: string;
  summary?: string;
}

export type AppState = 'idle' | 'uploading' | 'analyzing' | 'done' | 'error';
