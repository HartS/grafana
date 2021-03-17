import { FieldColorModeId, FieldConfigProperty, PanelPlugin } from '@grafana/data';
import { TimelinePanel } from './TimelinePanel';
import { TimelineFieldConfig, BarValueVisibility, graphFieldOptions } from '@grafana/ui';
import { addHideFrom, addLegendOptions } from '../timeseries/config';
import { defaultBarChartFieldConfig } from '@grafana/ui/src/components/BarChart/types';
import { Options } from './types';
import { TimelineMode } from '@grafana/ui/src/components/Timeline/types';

export const plugin = new PanelPlugin<Options, TimelineFieldConfig>(TimelinePanel)
  .useFieldConfig({
    standardOptions: {
      [FieldConfigProperty.Color]: {
        settings: {
          byValueSupport: true,
        },
        defaultValue: {
          mode: FieldColorModeId.PaletteClassic,
        },
      },
    },
    useCustomConfig: (builder) => {
      const cfg = defaultBarChartFieldConfig;

      builder
        .addSliderInput({
          path: 'lineWidth',
          name: 'Line width',
          defaultValue: cfg.lineWidth,
          settings: {
            min: 0,
            max: 10,
            step: 1,
          },
        })
        .addSliderInput({
          path: 'fillOpacity',
          name: 'Fill opacity',
          defaultValue: cfg.fillOpacity,
          settings: {
            min: 0,
            max: 100,
            step: 1,
          },
        })
        .addRadio({
          path: 'gradientMode',
          name: 'Gradient mode',
          defaultValue: graphFieldOptions.fillGradient[0].value,
          settings: {
            options: graphFieldOptions.fillGradient,
          },
        });

      // addAxisConfig(builder, cfg, true);
      addHideFrom(builder);
    },
  })
  .setPanelOptions((builder) => {
    builder
      .addRadio({
        path: 'mode',
        name: 'Mode',
        defaultValue: TimelineMode.Spans,
        settings: {
          options: [
            { label: 'Spans', value: TimelineMode.Spans },
            { label: 'Grid', value: TimelineMode.Grid },
          ],
        },
      })
      .addRadio({
        path: 'showValue',
        name: 'Show values',
        settings: {
          options: [
            { value: BarValueVisibility.Auto, label: 'Auto' },
            { value: BarValueVisibility.Always, label: 'Always' },
            { value: BarValueVisibility.Never, label: 'Never' },
          ],
        },
        defaultValue: BarValueVisibility.Auto,
      })
      .addSliderInput({
        path: 'barWidth',
        name: 'Bar width',
        defaultValue: 0.97,
        settings: {
          min: 0,
          max: 1,
          step: 0.01,
        },
      });

    addLegendOptions(builder);
  });
