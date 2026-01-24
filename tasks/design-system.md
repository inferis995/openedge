# Design System: Industrial Edge Middleware Frontend

## Overview

This design system defines the visual language and component patterns for the Industrial Edge Middleware web UI. It is based on the style reference in `style/STILE.txt` - a modern dashboard interface with a dark sidebar navigation and light content area.

## Color Palette

### Primary Colors

| Color | Tailwind Class | Hex Code | Usage |
|-------|---------------|----------|-------|
| Violet Primary | `bg-violet-600` | `#8b5cf6` | Primary buttons, active states, highlights |
| Violet Hover | `bg-violet-700` | `#7c3aed` | Primary button hover state |
| Violet Text | `text-violet-400` | `#a78bfa` | Sidebar branding, active navigation |

### Grayscale (Dark - Sidebar)

| Color | Tailwind Class | Hex Code | Usage |
|-------|---------------|----------|-------|
| Dark Background | `bg-gray-900` | `#111827` | Sidebar background |
| Dark Border | `border-gray-800` | `#1f2937` | Sidebar borders, dividers |
| Dark Text Primary | `text-gray-100` | `#f3f4f6` | Sidebar text, labels |
| Dark Text Secondary | `text-gray-400` | `#9ca3af` | Sidebar muted text |
| Dark Text Tertiary | `text-gray-500` | `#6b7280` | Sidebar hints, metadata |
| Dark Input Background | `bg-gray-800` | `#1f2937` | Input fields in dark context |

### Grayscale (Light - Main Content)

| Color | Tailwind Class | Hex Code | Usage |
|-------|---------------|----------|-------|
| Light Background | `bg-gray-50` | `#f9fafb` | Main content area background |
| Light Card Background | `bg-white` | `#ffffff` | Cards, panels, dialogs |
| Light Border | `border-gray-200` | `#e5e7eb` | Card borders, dividers |
| Light Text Primary | `text-gray-900` | `#111827` | Headings, primary text |
| Light Text Secondary | `text-gray-400` | `#9ca3af` | Labels, metadata |
| Light Input Background | `bg-white` | `#ffffff` | Input fields |

### Status Colors

| Status | Tailwind Class | Hex Code | Usage |
|--------|---------------|----------|-------|
| Green | `bg-green-500` | `#10b981` | Online status, success, good quality |
| Red | `bg-red-500` | `#ef4444` | Offline status, errors, active alarms |
| Yellow | `bg-yellow-400` | `#facc15` | RTN alarm state |
| Blue | `bg-blue-500` | `#3b82f6` | Acknowledged alarm state |
| Orange | `bg-orange-500` | `#f97316` | Medium priority |

## Typography

### Font Family

- **Primary**: Inter (Google Fonts)
- **Fallback**: System sans-serif fonts

```css
font-family: 'Inter', sans-serif;
```

### Import

```html
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&display=swap" rel="stylesheet">
```

### Type Scale

| Size | Tailwind Class | Weight | Usage |
|------|---------------|--------|-------|
| 20px | `text-xl` | 600 (semibold) | Page headings |
| 14px | `text-sm` | 600 (semibold) | Section headings |
| 14px | `text-sm` | 400 (regular) | Body text, labels |
| 12px | `text-xs` | 600 (semibold) | Uppercase labels |
| 11px | `text-[11px]` | 400 (regular) | Metadata, timestamps |

## Layout Structure

### Browser Window Container

```tsx
<div class="w-full max-w-7xl shadow-2xl rounded-2xl overflow-hidden border border-gray-300 bg-gray-100">
  {/* Browser window bar - optional */}
  <div class="flex items-center h-10 bg-gray-200 border-b border-gray-300 px-4">
    {/* Window controls */}
  </div>

  {/* Main content */}
  <div class="flex" style="height: 800px;">
    {/* Sidebar */}
    {/* Main content */}
  </div>
</div>
```

### Sidebar (Dark Mode)

```tsx
<aside class="w-80 flex-shrink-0 bg-gray-900 text-gray-100 flex flex-col h-full border-r border-gray-800">
  {/* Logo/Brand */}
  <div class="flex items-center h-16 px-6 border-b border-gray-800">
    <span class="text-lg font-bold tracking-tight text-violet-400">YourApp</span>
  </div>

  {/* Navigation */}
  <nav class="flex-1 px-6 py-4 space-y-2">
    {/* Nav items */}
  </nav>

  {/* Footer */}
  <div class="px-6 py-4 mt-auto border-t border-gray-800">
    {/* Footer content */}
  </div>
</aside>
```

### Main Content Area (Light Mode)

```tsx
<main class="flex-1 flex flex-col min-h-0 h-full bg-gray-50">
  {/* Header */}
  <header class="h-16 bg-white border-b border-gray-200 flex items-center px-8">
    <h1 class="text-xl font-semibold text-gray-900">Dashboard</h1>
  </header>

  {/* Content */}
  <section class="flex-1 p-8 overflow-y-auto">
    {/* Page content */}
  </section>
</main>
```

## Components

### Button (Primary)

```tsx
<button className="h-8 px-3 bg-violet-600 hover:bg-violet-700 text-white text-xs font-semibold rounded-md transition-colors">
  Button
</button>
```

### Button (Secondary/Navigation Link)

```tsx
<a className="flex items-center gap-3 px-2 py-2 bg-gray-800 rounded text-sm font-medium text-gray-100 hover:bg-violet-700 hover:text-white transition-colors">
  {/* Icon + Label */}
</a>
```

### Input Field (Dark)

```tsx
<input
  type="text"
  placeholder="Placeholder"
  className="flex-1 h-8 px-2 rounded-md bg-gray-800 border border-gray-700 text-gray-100 text-xs placeholder-gray-500 focus:outline-none focus:border-violet-500 transition-colors"
/>
```

### Input Field (Light)

```tsx
<input
  type="text"
  placeholder="Placeholder"
  className="h-8 px-2 rounded-md bg-white border border-gray-200 text-gray-900 text-xs placeholder-gray-400 focus:outline-none focus:border-violet-500 transition-colors"
/>
```

### Card (Light)

```tsx
<div className="p-4 bg-white rounded-lg border border-gray-200">
  {/* Card content */}
</div>
```

### Status Indicator (Dot)

```tsx
{/* Green - Online/Good */}
<span className="w-2 h-2 rounded-full bg-green-500"></span>

{/* Red - Offline/Error */}
<span className="w-2 h-2 rounded-full bg-red-400"></span>

{/* Yellow - Warning */}
<span className="w-2 h-2 rounded-full bg-yellow-400"></span>
```

### Progress Bar

```tsx
<div className="w-full bg-gray-700 rounded-full h-1.5">
  <div className="bg-violet-500 h-1.5 rounded-full" style="width: 40%"></div>
</div>
```

### Checkbox/Checkmark Icon

```tsx
<span className="inline-flex items-center justify-center w-4 h-4 rounded-full bg-violet-500">
  <svg className="w-2.5 h-2.5 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="3" d="M5 13l4 4L19 7"></path>
  </svg>
</span>
```

### Empty Circle (Unchecked)

```tsx
<span className="inline-block w-4 h-4 rounded-full border-2 border-gray-700"></span>
```

## Spacing Scale

| Token | Tailwind Class | Pixels | Usage |
|-------|---------------|--------|-------|
| xs | `px-2 py-1` | 8px | Small buttons, compact inputs |
| sm | `px-3 py-2` | 12px | Standard buttons, cards |
| md | `px-4 py-3` | 16px | Section padding |
| lg | `px-6 py-4` | 24px | Sidebar padding |
| xl | `px-8 py-6` | 32px | Page padding |

## Iconography

Use Lucide React icons (included with shadcn/ui):

```tsx
import { Settings, Plus, Share, ChevronRight } from 'lucide-react';

<Plus className="w-4 h-4" />
<Settings className="w-4 h-4" />
```

Standard icon sizes:
- Small: `w-3 h-3`
- Default: `w-4 h-4`
- Large: `w-5 h-5`

## Border Radius

| Size | Tailwind Class | Usage |
|------|---------------|-------|
| Small | `rounded-sm` | Chart bars |
| Medium | `rounded-md` | Buttons, inputs |
| Large | `rounded-lg` | Cards |
| XL | `rounded-2xl` | Main container |
| Full | `rounded-full` | Status dots, circular buttons |

## Shadows

| Level | Tailwind Class | Usage |
|-------|---------------|-------|
| Small | `shadow-sm` | Cards (light) |
| Medium | `shadow-md` | Dropdowns, dialogs |
| Large | `shadow-2xl` | Main container |

## Transitions

```css
/* Standard transition */
transition-colors

/* Duration */
duration-150 (fast)
duration-200 (default)
duration-300 (slow)
```

## Responsive Breakpoints

| Breakpoint | Width | Usage |
|------------|-------|-------|
| Mobile | `< 768px` | Stack layout vertically |
| Tablet | `768px - 1024px` | Adjust sidebar width |
| Desktop | `> 1024px` | Full layout |

## Page-Specific Layouts

### Login Page (Centered Card)

```tsx
<body class="bg-gray-200 min-h-screen flex items-center justify-center px-2 py-6">
  <div class="w-full max-w-md bg-white rounded-lg shadow-lg p-8">
    {/* Login form */}
  </div>
</body>
```

### Configuration Page (Split View)

```tsx
<div class="flex h-full">
  {/* Left: Tree View - 30% */}
  <div class="w-[30%] border-r border-gray-200 bg-white">
    {/* Tree navigation */}
  </div>

  {/* Right: Detail Panel - 70% */}
  <div class="flex-1 bg-gray-50">
    {/* Entity details */}
  </div>
</div>
```

### Trend Page (Chart + Controls)

```tsx
<div class="space-y-6">
  {/* Controls Bar */}
  <div className="flex gap-4 items-center">
    <select className="/* tag select */" />
    <input type="datetime-local" />
    <button>Query</button>
  </div>

  {/* Chart Area */}
  <div className="bg-white rounded-lg border border-gray-200 p-6">
    {/* Chart */}
  </div>
</div>
```

## Implementation Notes

1. **Always use Tailwind classes** - no inline styles or custom CSS unless absolutely necessary
2. **Follow the violet/gray color scheme** - don't introduce new accent colors
3. **Use Inter font** - import from Google Fonts
4. **Dark sidebar, light content** - maintain this contrast
5. **Consistent spacing** - use the spacing scale above
6. **Transitions on interactive elements** - buttons, links, inputs
7. **Status indicators use colored dots** - green (good/online), red (bad/offline), yellow (warning)
8. **Border radius consistency** - inputs and buttons use `rounded-md`, cards use `rounded-lg`
