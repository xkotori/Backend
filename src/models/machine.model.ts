import mongoose from "mongoose";
import { v4 as uuidv4 } from "uuid";
import { MachineDocument, MachineStatus } from "../types/machine";

const machineSchema = new mongoose.Schema({
  uuid: {
    type: String,
    unique: true,
    index: true,
  },
  created_at: {
    type: Number,
  },
  updated_at: {
    type: Number,
  },
  name: {
    type: String,
    required: true,
  },
  description: {
    type: String,
    required: false,
  },
  status: {
    type: String,
    required: true,
    default: MachineStatus.Unknown,
  },
  icon: {
    type: String,
    required: false,
  },
  access: [String],
  static_data: {
    hostname: String,
    public_ip: String,
    kernel_version: String,
    os_name: String,
    os_arch: String,
    os_version: String,
    cpu_model: String,
    cpu_base_frequency: String,
    cpu_cores: Number,
    cpu_threads: Number,
    total_memory: Number,
    last_sync: Number,
  },
});

machineSchema.pre("save", async function (this: MachineDocument, next) {
  if (this.isNew) {
    this.created_at = Date.now();
    this.uuid = uuidv4();
  }
  // Other middleware functions go here
  this.updated_at = Date.now();
  return next();
});

machineSchema.methods.setStatus = async function (this: MachineDocument, status: MachineStatus): Promise<MachineDocument> {
  this.status = status;
  return this.save();
};

// machineSchema.methods.updateStaticData = async function (this: MachineDocument, newValues: string): Promise<MachineDocument> {
//   this.password = newValue;
//   return this.save();
// };

export default mongoose.model<MachineDocument>("Machine", machineSchema);