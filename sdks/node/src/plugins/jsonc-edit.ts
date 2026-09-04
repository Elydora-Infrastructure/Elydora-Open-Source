import { applyEdits, modify, type FormattingOptions, type JSONPath } from 'jsonc-parser';

function formatting(raw: string): FormattingOptions {
  const indentation = /\r?\n([ \t]+)\S/.exec(raw)?.[1];
  const insertSpaces = !indentation?.includes('\t');
  return {
    eol: raw.includes('\r\n') ? '\r\n' : '\n',
    insertSpaces,
    tabSize: insertSpaces ? Math.max(1, indentation?.length ?? 2) : 1,
  };
}

export function changeJsonc(
  raw: string,
  jsonPath: JSONPath,
  value: unknown,
  isArrayInsertion = false,
): string {
  return applyEdits(raw, modify(raw, jsonPath, value, {
    formattingOptions: formatting(raw),
    isArrayInsertion,
  }));
}
