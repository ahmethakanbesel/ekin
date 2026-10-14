import { createFormHook, createFormHookContexts } from "@tanstack/react-form"

// The app's form hook. Plain useForm works too; useAppForm is needed where
// a form renders a shared field group (withFieldGroup), such as the
// outputs and document references the pipeline and request forms share.
const { fieldContext, formContext } = createFormHookContexts()

export const { useAppForm, withFieldGroup } = createFormHook({
  fieldContext,
  formContext,
  fieldComponents: {},
  formComponents: {},
})
