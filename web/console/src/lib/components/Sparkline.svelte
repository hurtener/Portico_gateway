<script lang="ts">
  /**
   * Minimal pure-SVG sparkline. No charting lib; renders a smooth line
   * over an even time grid. Pass `data` as numbers; the component scales
   * to the inferred min/max with a small bottom margin so flat lines
   * stay visible.
   */
  export let data: number[] = [];
  export let width = 120;
  export let height = 32;
  export let stroke = 'var(--color-accent-primary)';
  export let fill = 'var(--color-accent-primary-subtle)';

  $: points = computePoints(data, width, height);

  function computePoints(d: number[], w: number, h: number): { line: string; area: string } {
    if (d.length === 0) return { line: '', area: '' };
    if (d.length === 1) {
      const x = w / 2;
      const y = h / 2;
      return { line: `M${x - 1},${y} L${x + 1},${y}`, area: '' };
    }
    const max = Math.max(...d);
    const min = Math.min(...d);
    const span = Math.max(max - min, 1);
    const stepX = w / (d.length - 1);
    const pad = h * 0.15;
    const usable = h - pad * 2;
    const scaled = d.map((v, i) => {
      const x = i * stepX;
      const y = h - pad - ((v - min) / span) * usable;
      return [x, y] as const;
    });
    const line = scaled
      .map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`)
      .join(' ');
    const area =
      `M${scaled[0][0].toFixed(1)},${h} ` +
      scaled.map(([x, y]) => `L${x.toFixed(1)},${y.toFixed(1)}`).join(' ') +
      ` L${scaled[scaled.length - 1][0].toFixed(1)},${h} Z`;
    return { line, area };
  }
</script>

<svg
  class="sparkline"
  {width}
  {height}
  viewBox={`0 0 ${width} ${height}`}
  role="img"
  aria-label="Trend"
  preserveAspectRatio="none"
>
  {#if points.area}
    <path d={points.area} {fill} opacity="0.7" />
  {/if}
  {#if points.line}
    <path
      d={points.line}
      fill="none"
      {stroke}
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  {/if}
</svg>

<style>
  .sparkline {
    display: inline-block;
    vertical-align: middle;
  }
</style>
