"use client";

import { errorMessage, hasErrorCode, useCheckout } from "@scaffoldapp/shared";
import { Button, toast, type ButtonProps } from "@scaffoldapp/ui-web";
import { useState } from "react";

export interface CheckoutButtonProps extends Omit<ButtonProps, "onClick" | "loading" | "asChild"> {
  priceKey: string;
  quantity?: number;
  /** App path the provider returns to after paying (must exist; checkout_id is appended). */
  successPath?: string;
  cancelPath?: string;
}

/**
 * Starts hosted Checkout for one catalog price and leaves for the provider's page.
 * Nothing is granted here: the webhook confirms payment and the success page polls.
 * Web only sells what the store rules allow on web (anything); the mobile app decides
 * separately (see the mobile plans screen).
 */
export function CheckoutButton({ priceKey, quantity, successPath = "/payments/success", cancelPath = "/payments/cancel", children, ...button }: CheckoutButtonProps) {
  const checkout = useCheckout();
  const [leaving, setLeaving] = useState(false);
  return (
    <Button
      {...button}
      loading={checkout.isPending || leaving}
      onClick={() =>
        checkout.mutate(
          { price_key: priceKey, ...(quantity ? { quantity } : {}), success_path: successPath, cancel_path: cancelPath },
          {
            onSuccess: (res) => {
              setLeaving(true);
              window.location.assign(res.url);
            },
            onError: (e) => {
              if (hasErrorCode(e, "payments_unconfigured")) toast.error("Payments aren't set up yet.");
              else toast.error(errorMessage(e, "Couldn't open checkout. Try again."));
            },
          },
        )
      }
    >
      {children}
    </Button>
  );
}
