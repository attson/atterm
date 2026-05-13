/// <reference types="vite/client" />

declare module '*.vue' {
    import type {DefineComponent} from 'vue'
    const component: DefineComponent<{}, {}, any>
    export default component
}

declare module 'node:fs' {
    export function readFileSync(path: string, encoding: string): string
}
