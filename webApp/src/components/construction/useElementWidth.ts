/**
 * The rendered width of one element, kept current with a ResizeObserver — how
 * the Code tab decides between the signature list and the canvas, and how the
 * scenario browser knows it sits in the narrow pane. 0 until the first measure,
 * which every caller reads as the narrow (safe) case.
 */
import { useCallback, useEffect, useState } from 'react';

export function useElementWidth(): [(node: HTMLElement | null) => void, number] {
  const [node, setNode] = useState<HTMLElement | null>(null);
  const [width, setWidth] = useState(0);
  const ref = useCallback((next: HTMLElement | null): void => {
    setNode(next);
  }, []);
  useEffect(() => {
    if (node === null) return undefined;
    const measure = (): void => {
      setWidth(Math.round(node.getBoundingClientRect().width));
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return (): void => {
      observer.disconnect();
    };
  }, [node]);
  return [ref, width];
}
