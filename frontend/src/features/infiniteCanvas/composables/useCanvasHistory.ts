import { ref, shallowRef, type Ref } from 'vue'

export interface CanvasHistory<T> {
  canUndo: Ref<boolean>
  canRedo: Ref<boolean>
  push: (snapshot: T) => void
  undo: (current: T) => T | undefined
  redo: (current: T) => T | undefined
  clear: () => void
}

export function useCanvasHistory<T>(limit = 50): CanvasHistory<T> {
  const past = shallowRef<T[]>([])
  const future = shallowRef<T[]>([])
  const canUndo = ref(false)
  const canRedo = ref(false)
  const sync = () => { canUndo.value = past.value.length > 0; canRedo.value = future.value.length > 0 }

  const push = (snapshot: T) => {
    past.value.push(snapshot)
    if (past.value.length > limit) past.value.shift()
    future.value = []
    sync()
  }
  const undo = (current: T) => {
    const snapshot = past.value.pop()
    if (snapshot !== undefined) future.value.push(current)
    sync()
    return snapshot
  }
  const redo = (current: T) => {
    const snapshot = future.value.pop()
    if (snapshot !== undefined) past.value.push(current)
    sync()
    return snapshot
  }
  const clear = () => { past.value = []; future.value = []; sync() }
  return { canUndo, canRedo, push, undo, redo, clear }
}
