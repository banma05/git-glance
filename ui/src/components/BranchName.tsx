// BranchName writes a branch name so that a narrow row cuts it from the front.
// A name that fits is shown whole. When it does not, the folders before the
// last "/" are dropped whole for a "…" (never a sliver of them), then the last
// segment (with its "/") is cut at its end. CSS does it (app.css .bname): the
// box is rtl and wraps, the last segment comes first in the DOM, so the folders
// are what wraps to the second line, which is never shown. What is shown is
// hidden from assistive technology, which gets the whole name (`full`, the
// shown name by default) instead.
export function BranchName({ name, full = name, class: cls = '' }: { name: string; full?: string; class?: string }) {
  const cut = name.lastIndexOf('/');
  const prefix = cut > 0 ? name.slice(0, cut) : '';
  const last = cut > 0 ? name.slice(cut) : name;
  return (
    <span class={`bname${prefix ? '' : ' bname-plain'}${cls ? ' ' + cls : ''}`}>
      <span class="bname-last" aria-hidden="true">
        {last}
      </span>
      {prefix && (
        <span class="bname-pre" aria-hidden="true">
          <bdi>{prefix}</bdi>
        </span>
      )}
      <span class="vh">{full}</span>
    </span>
  );
}
