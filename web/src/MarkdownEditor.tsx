import { useId, useState } from "react";
import type { Ref, TextareaHTMLAttributes } from "react";
import { MarkdownContent } from "./MarkdownContent";

interface Props extends Omit<
  TextareaHTMLAttributes<HTMLTextAreaElement>,
  "value"
> {
  value: string;
  label: string;
  textareaRef?: Ref<HTMLTextAreaElement>;
  preview?: boolean;
  onPreviewChange?: (preview: boolean) => void;
}

export function MarkdownEditor({
  value,
  label,
  textareaRef,
  preview,
  onPreviewChange,
  ...textareaProps
}: Props) {
  const generated = useId();
  const [localPreview, setLocalPreview] = useState(false);
  const showingPreview = preview ?? localPreview;
  const setPreview = onPreviewChange ?? setLocalPreview;
  const editorID = textareaProps.id || `${generated}-input`;
  const tabs = [
    { name: "Write", preview: false },
    { name: "Preview", preview: true },
  ];
  return (
    <div className="markdown-editor">
      <div
        className="markdown-editor-tabs"
        role="tablist"
        aria-label={`${label} format`}
      >
        {tabs.map((tab) => (
          <button
            type="button"
            role="tab"
            key={tab.name}
            id={`${generated}-${tab.name}`}
            aria-selected={showingPreview === tab.preview}
            aria-controls={`${generated}-${tab.name}-panel`}
            tabIndex={showingPreview === tab.preview ? 0 : -1}
            onClick={() => setPreview(tab.preview)}
            onKeyDown={(event) => {
              if (
                !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)
              )
                return;
              event.preventDefault();
              const next =
                event.key === "Home"
                  ? false
                  : event.key === "End"
                    ? true
                    : !showingPreview;
              setPreview(next);
              document
                .getElementById(`${generated}-${next ? "Preview" : "Write"}`)
                ?.focus();
            }}
          >
            {tab.name}
          </button>
        ))}
        <span className="markdown-editor-hint">Markdown supported</span>
      </div>
      <div
        role="tabpanel"
        id={`${generated}-Write-panel`}
        aria-labelledby={`${generated}-Write`}
        hidden={showingPreview}
      >
        <textarea
          {...textareaProps}
          id={editorID}
          ref={textareaRef}
          value={value}
          aria-label={textareaProps["aria-label"] || label}
        />
      </div>
      <div
        className="markdown-preview"
        role="tabpanel"
        id={`${generated}-Preview-panel`}
        aria-labelledby={`${generated}-Preview`}
        hidden={!showingPreview}
        tabIndex={0}
      >
        {showingPreview &&
          (value.trim() ? (
            <MarkdownContent>{value}</MarkdownContent>
          ) : (
            <p className="muted">Nothing to preview</p>
          ))}
      </div>
    </div>
  );
}
