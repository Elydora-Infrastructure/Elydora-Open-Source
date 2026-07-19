'use client';

import { useEffect, useRef, useId } from 'react';
import { createPortal } from 'react-dom';

interface ModalProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  children: React.ReactNode;
  width?: string;
  dismissible?: boolean;
}

export default function Modal({
  isOpen,
  onClose,
  title,
  children,
  width = 'max-w-lg',
  dismissible = true,
}: ModalProps) {
  const titleId = useId();
  const overlayRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<Element | null>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  // Save previous focus on open; restore on unmount (close)
  useEffect(() => {
    if (!isOpen) return;

    previousFocusRef.current = document.activeElement;
    document.body.style.overflow = 'hidden';
    requestAnimationFrame(() => {
      dialogRef.current?.focus();
    });

    return () => {
      document.body.style.overflow = '';
      if (previousFocusRef.current instanceof HTMLElement) {
        previousFocusRef.current.focus();
      }
    };
  }, [isOpen]);

  // Keydown listener — uses ref so it never changes identity
  useEffect(() => {
    if (!isOpen) return;

    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && dismissible) {
        onCloseRef.current();
        return;
      }

      if (e.key === 'Tab' && dialogRef.current) {
        const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
        );
        if (focusable.length === 0) return;

        const first = focusable[0]!;
        const last = focusable[focusable.length - 1]!;

        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    };

    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [dismissible, isOpen]);

  if (!isOpen) return null;

  return createPortal(
    <div
      ref={overlayRef}
      className="fixed inset-0 md:left-[260px] z-[100] flex items-center justify-center modal-overlay"
      onClick={(e) => {
        if (dismissible && e.target === overlayRef.current) onCloseRef.current();
      }}
      role="presentation"
    >
      <div
        ref={dialogRef}
        className={`${width} w-full mx-4 bg-bg border border-ink relative fade-in`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        {/* Crosshair corners */}
        <span className="crosshair ch-tl" aria-hidden="true" />
        <span className="crosshair ch-tr" aria-hidden="true" />
        <span className="crosshair ch-bl" aria-hidden="true" />
        <span className="crosshair ch-br" aria-hidden="true" />

        {/* Header */}
        <div className="flex items-center justify-between px-4 sm:px-6 py-4 border-b border-border">
          <h2 id={titleId} className="font-sans text-lg font-semibold tracking-tight text-ink min-w-0 truncate">
            {title}
          </h2>
          {dismissible && (
            <button
              onClick={() => onCloseRef.current()}
              className="w-8 h-8 flex items-center justify-center text-ink-dim hover:text-ink transition-colors"
              aria-label="Close"
            >
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
                <path d="M1 1l12 12M13 1L1 13" />
              </svg>
            </button>
          )}
        </div>

        {/* Content */}
        <div className="px-4 sm:px-6 py-5 max-h-[calc(100vh-8rem)] overflow-y-auto">
          {children}
        </div>
      </div>
    </div>,
    document.body,
  );
}
