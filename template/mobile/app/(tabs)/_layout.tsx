import { TabBar, useTheme } from "@scaffoldapp/ui-native";
import { Tabs } from "expo-router/js-tabs";
import { House, Settings } from "lucide-react-native";
// scaffold:imports

/**
 * Bottom tabs: at most 5, each a labelled icon (concrete/design.md). Features insert their
 * `<Tabs.Screen>` above the tabs anchor (before Settings, so Settings stays last). A sixth
 * destination goes into Settings or a Home entry, never a scrolling tab bar.
 */
export default function TabsLayout() {
  const theme = useTheme();
  return (
    <Tabs tabBar={(props) => <TabBar {...props} />} screenOptions={{ headerShown: false, sceneStyle: { backgroundColor: theme.colors.canvas } }}>
      <Tabs.Screen name="index" options={{ title: "Home", tabBarIcon: ({ color, size }) => <House color={color} size={size} strokeWidth={2} /> }} />
      {/* scaffold:tabs */}
      <Tabs.Screen name="settings" options={{ title: "Settings", tabBarIcon: ({ color, size }) => <Settings color={color} size={size} strokeWidth={2} /> }} />
    </Tabs>
  );
}
