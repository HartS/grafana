import { PanelPlugin } from '@grafana/data';
import { TablePanel } from './TablePanel';
import { PanelOptions, PanelFieldConfig, defaultPanelOptions, defaultPanelFieldConfig } from './models.gen';
import { tableMigrationHandler, tablePanelChangedHandler } from './migrations';
import { TableCellDisplayMode } from '@grafana/ui';

export const plugin = new PanelPlugin<PanelOptions, PanelFieldConfig>(TablePanel)
  .setPanelChangeHandler(tablePanelChangedHandler)
  .setMigrationHandler(tableMigrationHandler)
  .setNoPadding()
  .useFieldConfig({
    useCustomConfig: (builder) => {
      builder
        .addNumberInput({
          path: 'width',
          name: 'Column width',
          settings: {
            placeholder: 'auto',
            min: 20,
            max: 300,
          },
          shouldApply: () => true,
          defaultValue: defaultPanelFieldConfig.width,
        })
        .addRadio({
          path: 'align',
          name: 'Column alignment',
          settings: {
            options: [
              { label: 'auto', value: null },
              { label: 'left', value: 'left' },
              { label: 'center', value: 'center' },
              { label: 'right', value: 'right' },
            ],
          },
          defaultValue: defaultPanelFieldConfig.align,
        })
        .addSelect({
          path: 'displayMode',
          name: 'Cell display mode',
          description: 'Color text, background, show as gauge, etc',
          settings: {
            options: [
              { value: TableCellDisplayMode.Auto, label: 'Auto' },
              { value: TableCellDisplayMode.ColorText, label: 'Color text' },
              { value: TableCellDisplayMode.ColorBackground, label: 'Color background' },
              { value: TableCellDisplayMode.GradientGauge, label: 'Gradient gauge' },
              { value: TableCellDisplayMode.LcdGauge, label: 'LCD gauge' },
              { value: TableCellDisplayMode.BasicGauge, label: 'Basic gauge' },
              { value: TableCellDisplayMode.JSONView, label: 'JSON View' },
              { value: TableCellDisplayMode.Image, label: 'Image' },
            ],
          },
          defaultValue: defaultPanelFieldConfig.displayMode,
        })
        .addBooleanSwitch({
          path: 'filterable',
          name: 'Column filter',
          description: 'Enables/disables field filters in table',
          defaultValue: defaultPanelFieldConfig.filterable,
        });
    },
  })
  .setPanelOptions((builder) => {
    builder.addBooleanSwitch({
      path: 'showHeader',
      name: 'Show header',
      description: "To display table's header or not to display",
      defaultValue: defaultPanelOptions.showHeader,
    });
  });
