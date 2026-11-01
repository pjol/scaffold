"use client";

import { Avatar, type AvatarProps } from "@scaffoldapp/ui-web";
import Image from "next/image";
import { isOptimisable } from "@/lib/images";

/**
 * ui-web `Avatar` with photos through `next/image` (optimised for the bucket and Clerk;
 * plain otherwise). Pass `eager` when the avatar is the screen's main image (profile, user
 * detail) so it isn't lazy-loaded above the fold.
 */
export function AppAvatar({ eager, ...props }: Omit<AvatarProps, "renderImage"> & { eager?: boolean }) {
  return (
    <Avatar
      {...props}
      renderImage={({ src, alt, size, className, onError }) => (
        <Image
          src={src}
          alt={alt}
          width={size}
          height={size}
          sizes={`${size}px`}
          className={className}
          onError={onError}
          loading={eager ? "eager" : "lazy"}
          unoptimized={!isOptimisable(src)}
        />
      )}
    />
  );
}
