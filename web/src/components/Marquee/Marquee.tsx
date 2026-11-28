import { useEffect, useRef, type ReactNode } from 'react';
import { slideBack, slideOut, stopSlide } from '@/lib/marquee';
import styles from './Marquee.module.css';

/**
 * One line that is cut with an ellipsis when it does not fit, and slides to its end while the
 * pointer is over it or a control inside has the focus (and back when it leaves). Under reduced
 * motion it jumps instead. `title` is the full text, for the tooltip of the browser.
 */
export function Marquee({
  className,
  title,
  children,
}: {
  className?: string;
  title?: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const el = ref.current;
    return () => {
      if (el) stopSlide(el);
    };
  }, []);
  return (
    <span
      ref={ref}
      className={className ? `${styles.mq} ${className}` : styles.mq}
      title={title}
      onMouseEnter={(e) => slideOut(e.currentTarget)}
      onMouseLeave={(e) => slideBack(e.currentTarget)}
      onFocus={(e) => slideOut(e.currentTarget)}
      onBlur={(e) => {
        if (!e.currentTarget.matches(':hover')) slideBack(e.currentTarget);
      }}
    >
      {children}
    </span>
  );
}
