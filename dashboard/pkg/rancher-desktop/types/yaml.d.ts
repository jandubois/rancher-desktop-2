// The package build loads YAML files with js-yaml-loader.
declare module '*.yaml' {
  const content: Record<string, any>;

  export default content;
}
