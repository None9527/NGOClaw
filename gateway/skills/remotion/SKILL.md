# Remotion Skill

Expert guide for creating programmatic videos with Remotion (React). Follow these "Master Patterns" to ensure high-performance, maintainable video code.

## 🧠 Master Wisdom: Core Principles

- **Frame-as-Function**: View every component as a function of the current frame index (`useCurrentFrame()`).
- **Deterministic Rendering**: Ensure the same frame index always produces the exact same visual output. Avoid `Math.random()` or `new Date()` inside components; use Remotion's `random()` or fixed timestamps.
- **Composition over Editing**: Build videos by nesting `Composition` and `Sequence` components rather than thinking in "tracks".

## 🛠️ Animation Patterns

### 1. Spring-based Movement
Always prefer `spring()` for natural feel:
```javascript
const frame = useCurrentFrame();
const { fps } = useVideoConfig();
const scale = spring({
  frame,
  fps,
  config: { stiffness: 100 }
});
```

### 2. Interpolation
Map frames to values cleanly:
```javascript
const opacity = interpolate(frame, [0, 20], [0, 1], {
  extrapolateRight: "clamp",
});
```

## 🚀 Performance Checklist (Anti-Patterns to Avoid)

- [ ] **NO Heavy Logic in Render**: Do not fetch data or perform heavy processing inside components. Pre-calculate or use `delayRender`.
- [ ] **Memoize Props**: If using the `<Player>`, wrap `inputProps` in `useMemo`.
- [ ] **Asset Reference**: Use `staticFile('path/to/asset')` for everything in the `public/` folder.
- [ ] **Video Metadata**: Use `@remotion/media-utils` to pre-fetch duration/dimensions before the render starts.

## ⚠️ Common Pitfalls (Masters' Lessons)

- **GIF Sync**: Standard `<img>` tags for GIFs will loop independently of the Remotion timeline. Use the specialized GIF components that sync with `frame`.
- **Z-Index**: Since it's web-based, rely on CSS z-index and DOM order for layering.
- **Font Loading**: Use `continueRender` and `delayRender` to ensure custom fonts are fully loaded before capturing frames.

## 💻 Workflow Commands

- `npx remotion preview`: Start the Studio.
- `npx remotion render <comp-id> out/video.mp4`: Final export.
