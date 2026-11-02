import { useMemo } from "react";
import Markdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import { SyntaxText } from "./SyntaxText";
import { useSyntax } from "./useSyntax";
import { codeFencePath } from "./syntax-language";
import type { SyntaxRequest } from "./syntax-types";
import "./markdown.css";

const schema = {
  ...defaultSchema,
  strip: [
    ...(defaultSchema.strip || []),
    "style",
    "iframe",
    "object",
    "embed",
    "form",
  ],
};

export function githubImageURL(value: string | undefined): string | undefined {
  if (!value) return;
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" || url.username || url.password || url.port)
      return;
    if (
      url.hostname.endsWith(".githubusercontent.com") ||
      url.hostname === "github-production-user-asset-6210df.s3.amazonaws.com" ||
      (url.hostname === "github.com" &&
        url.pathname.startsWith("/user-attachments/"))
    )
      return url.href;
  } catch {
    // Relative image URLs cannot be resolved safely without repository context.
  }
}

function HighlightedCode({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  const text = String(children ?? "");
  const language = /^language-([a-zA-Z0-9_-]+)$/.exec(className || "")?.[1];
  const path = language ? codeFencePath(language) : undefined;
  const request = useMemo<SyntaxRequest | undefined>(
    () => (path ? { kind: "source", path, code: text } : undefined),
    [path, text],
  );
  const syntax = useSyntax(request);
  return (
    <code className={className}>
      {text.split("\n").map((line, index, lines) => (
        <span key={index}>
          <SyntaxText text={line} spans={syntax.lines[index]} />
          {index < lines.length - 1 ? "\n" : ""}
        </span>
      ))}
    </code>
  );
}

export function MarkdownContent({ children }: { children: string }) {
  return (
    <div className="markdown-body">
      <Markdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, schema]]}
        urlTransform={defaultUrlTransform}
        components={{
          a: ({ children, href, title }) => (
            <a
              href={href}
              title={title}
              target={href?.startsWith("#") ? undefined : "_blank"}
              rel="noopener noreferrer"
            >
              {children}
            </a>
          ),
          img: ({ src, alt, title, width, height }) => {
            const image = githubImageURL(src);
            if (!image)
              return src && /^https?:\/\//i.test(src) ? (
                <a href={src} target="_blank" rel="noopener noreferrer">
                  {alt || "View external image"}
                </a>
              ) : (
                <span>{alt || "Image unavailable"}</span>
              );
            return (
              <img
                src={image}
                alt={alt || ""}
                title={title}
                width={width}
                height={height}
                loading="lazy"
                referrerPolicy="no-referrer"
              />
            );
          },
          code: ({ className, children }) => (
            <HighlightedCode className={className}>{children}</HighlightedCode>
          ),
          input: ({ checked }) => (
            <input type="checkbox" checked={!!checked} disabled readOnly />
          ),
        }}
      >
        {children}
      </Markdown>
    </div>
  );
}
