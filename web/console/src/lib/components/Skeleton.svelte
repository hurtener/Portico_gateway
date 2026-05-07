<script lang="ts">
  export let width: string = '100%';
  export let height: string = '16px';
  export let radius: string = 'var(--radius-sm)';
  export let circle = false;
</script>

<span
  class="sk"
  class:circle
  style="--sk-w:{width};--sk-h:{height};--sk-r:{radius}"
  aria-hidden="true"
></span>

<style>
  .sk {
    display: inline-block;
    width: var(--sk-w);
    height: var(--sk-h);
    border-radius: var(--sk-r);
    background: linear-gradient(
      90deg,
      var(--color-bg-subtle) 0%,
      var(--color-bg-elevated) 40%,
      var(--color-bg-subtle) 80%
    );
    background-size: 200% 100%;
    animation: sk-shimmer 1.6s linear infinite;
  }
  .circle {
    border-radius: 999px;
  }
  @keyframes sk-shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sk {
      animation: none;
    }
  }
</style>
