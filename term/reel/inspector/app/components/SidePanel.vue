<script setup lang="ts">
const { panelTab: tab } = useInspector()
</script>

<template>
  <Tabs v-model="tab" class="flex h-full flex-col gap-0">
    <div class="flex h-10 shrink-0 items-center border-b px-2">
      <TabsList variant="line" class="h-7 gap-1">
        <TabsTrigger value="event" class="px-2 text-[13px]">
          Event
        </TabsTrigger>
        <TabsTrigger value="state" class="px-2 text-[13px]">
          Terminal
        </TabsTrigger>
        <TabsTrigger value="colors" class="px-2 text-[13px]">
          Colors
        </TabsTrigger>
        <TabsTrigger value="recording" class="px-2 text-[13px]">
          Recording
        </TabsTrigger>
      </TabsList>
    </div>
    <div class="min-h-0 flex-1 overflow-y-auto scrollbar-thin">
      <TabsContent value="event">
        <EventDetail />
      </TabsContent>
      <TabsContent value="state">
        <StatePanel />
      </TabsContent>
      <TabsContent value="colors">
        <ColorsPanel />
      </TabsContent>
      <TabsContent value="recording">
        <RecordingPanel />
      </TabsContent>
    </div>
  </Tabs>
</template>
