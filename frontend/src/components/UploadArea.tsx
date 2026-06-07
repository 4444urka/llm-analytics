import { useState, useRef, useCallback } from "react";

interface UploadAreaProps {
  onFileSelect: (file: File) => void;
  onError?: (error: string) => void;
  disabled?: boolean;
  selectedFile?: File | null;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1048576).toFixed(1)} MB`;
}

const ALLOWED_EXTS = [".csv", ".xlsx", ".xls"];

function validateFile(file: File): string | null {
  const ext = file.name.toLowerCase();
  if (!ALLOWED_EXTS.some((e) => ext.endsWith(e))) {
    return "Only CSV and Excel files are supported";
  }
  return null;
}

const UploadArea: React.FC<UploadAreaProps> = ({
  onFileSelect,
  onError,
  disabled = false,
  selectedFile,
}) => {
  const [dragActive, setDragActive] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleDrag = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.type === "dragenter" || e.type === "dragover") {
      setDragActive(true);
    } else if (e.type === "dragleave") {
      setDragActive(false);
    }
  }, []);

  const processFile = useCallback(
    (file: File) => {
      const err = validateFile(file);
      if (err) {
        onError?.(err);
        return;
      }
      onError?.("");
      onFileSelect(file);
    },
    [onFileSelect, onError],
  );

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      setDragActive(false);

      if (disabled) return;

      const droppedFile = e.dataTransfer.files?.[0];
      if (droppedFile) {
        processFile(droppedFile);
      }
    },
    [disabled, processFile],
  );

  const handleFileChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (disabled) return;
      const selected = e.target.files?.[0];
      if (selected) {
        processFile(selected);
      }
    },
    [disabled, processFile],
  );

  const handleClick = useCallback(() => {
    if (!disabled) {
      fileInputRef.current?.click();
    }
  }, [disabled]);

  return (
    <div>
      <div
        className={`upload-area ${dragActive ? "active" : ""}`}
        onDragEnter={handleDrag}
        onDragLeave={handleDrag}
        onDragOver={handleDrag}
        onDrop={handleDrop}
        onClick={handleClick}
      >
        <div className="upload-area-icon">
          {dragActive ? "\u2B07" : "\uD83D\uDCC1"}
        </div>
        <p>
          {dragActive
            ? "Drop it here"
            : "Click or drag & drop your CSV/Excel file"}
        </p>
        <small>Max 50MB</small>
        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,.xlsx,.xls"
          onChange={handleFileChange}
          style={{ display: "none" }}
        />
      </div>

      {selectedFile && (
        <div className="file-selected">
          {selectedFile.name} ({formatSize(selectedFile.size)})
        </div>
      )}
    </div>
  );
};

export default UploadArea;
