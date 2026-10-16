import { Slider as SliderPrimitive } from "@base-ui/react/slider"

import { cn } from "@/lib/utils"

// Stock base-vega slider, kept local for a scalar value (one thumb) and an
// accessible name and value text on the thumb's hidden range input.
function Slider({
  className,
  thumbLabel,
  valueText,
  ...props
}: SliderPrimitive.Root.Props & {
  /** The accessible name of the thumb (its hidden range input). */
  thumbLabel?: string
  /** How the value reads aloud, e.g. "1.25 times". */
  valueText?: string
}) {
  return (
    <SliderPrimitive.Root
      className={cn("data-horizontal:w-full data-vertical:h-full", className)}
      data-slot="slider"
      thumbAlignment="edge"
      {...props}
    >
      <SliderPrimitive.Control className="relative flex w-full touch-none items-center select-none data-disabled:opacity-50 data-horizontal:h-5 data-vertical:h-full data-vertical:min-h-40 data-vertical:w-auto data-vertical:flex-col">
        <SliderPrimitive.Track
          data-slot="slider-track"
          className="relative grow overflow-hidden rounded-full bg-muted select-none data-horizontal:h-1.5 data-horizontal:w-full data-vertical:h-full data-vertical:w-1.5"
        >
          <SliderPrimitive.Indicator
            data-slot="slider-range"
            className="bg-primary select-none data-horizontal:h-full data-vertical:w-full"
          />
        </SliderPrimitive.Track>
        <SliderPrimitive.Thumb
          data-slot="slider-thumb"
          getAriaLabel={thumbLabel ? () => thumbLabel : undefined}
          aria-valuetext={valueText}
          className="block size-4 shrink-0 rounded-full border border-primary bg-white shadow-sm transition-[color,box-shadow] select-none hover:ring-4 hover:ring-ring/50 focus-visible:ring-4 focus-visible:ring-ring focus-visible:outline-hidden data-disabled:pointer-events-none"
        />
      </SliderPrimitive.Control>
    </SliderPrimitive.Root>
  )
}

export { Slider }
