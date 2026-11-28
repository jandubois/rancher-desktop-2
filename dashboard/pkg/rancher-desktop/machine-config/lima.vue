<script>
import { LabeledInput } from '@components/Form/LabeledInput';
import { _CREATE } from '@shell/config/query-params';

export default {
  name: 'MachineConfigLima',

  components: { LabeledInput },

  emits: ['validationChanged'],

  props: {
    // The LimaMachineTemplate of the pool
    value: {
      type:     Object,
      required: true,
    },

    mode: {
      type:    String,
      default: _CREATE,
    },

    busy: {
      type:    Boolean,
      default: false,
    },

    disabled: {
      type:    Boolean,
      default: false,
    },
  },

  created() {
    this.value.spec = this.value.spec || {};
    this.value.spec.template = this.value.spec.template || {};
    this.value.spec.template.spec = this.value.spec.template.spec || {};
    this.$emit('validationChanged', true);
  },

  computed: {
    spec() {
      return this.value.spec.template.spec;
    },

    cpus: {
      get() {
        return this.spec.cpus;
      },
      set(neu) {
        const cpus = parseInt(neu, 10);

        if (Number.isNaN(cpus)) {
          delete this.spec.cpus;
        } else {
          this.spec.cpus = cpus;
        }
      },
    },

    memory: {
      get() {
        return this.spec.memory;
      },
      set(neu) {
        this.setQuantity('memory', neu);
      },
    },

    disk: {
      get() {
        return this.spec.disk;
      },
      set(neu) {
        this.setQuantity('disk', neu);
      },
    },
  },

  methods: {
    // An empty quantity leaves the field unset, so the provider applies its default.
    setQuantity(field, neu) {
      const quantity = `${ neu ?? '' }`.trim();

      if (quantity) {
        this.spec[field] = quantity;
      } else {
        delete this.spec[field];
      }
    },
  },
};
</script>

<template>
  <div class="row mb-20">
    <div class="col span-4">
      <LabeledInput
        v-model:value="cpus"
        type="number"
        min="1"
        :mode="mode"
        :disabled="disabled || busy"
        :label="t('lima.machineConfig.cpus.label')"
      />
    </div>
    <div class="col span-4">
      <LabeledInput
        v-model:value="memory"
        :mode="mode"
        :disabled="disabled || busy"
        :label="t('lima.machineConfig.memory.label')"
        :placeholder="t('lima.machineConfig.memory.placeholder')"
        :tooltip="t('lima.machineConfig.quantityTooltip')"
      />
    </div>
    <div class="col span-4">
      <LabeledInput
        v-model:value="disk"
        :mode="mode"
        :disabled="disabled || busy"
        :label="t('lima.machineConfig.disk.label')"
        :placeholder="t('lima.machineConfig.disk.placeholder')"
        :tooltip="t('lima.machineConfig.quantityTooltip')"
      />
    </div>
  </div>
</template>
